package network

import (
	"encoding/json"
	"fmt"
	"mydocker/pkg/container"
	"net"
	"os"
	"os/exec"
	"path"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

const ipamDefaultAllocatorPath = "/var/run/mydocker/network/ipam/subnet.json"

// 存放 IP 地址分配信息
type IPAM struct {
	// 分配文件存放位置
	SubnetAllocatorPath string
	// 网段和位图算法的数组 map，key 是网段，value 是分配的位图数组
	Subnets *map[string]string
}

var ipAllocator = &IPAM{
	SubnetAllocatorPath: ipamDefaultAllocatorPath,
}

// 从指定的 subnet 网段中分配 ip 地址
func (ipam *IPAM) Allocate(subnet *net.IPNet) (ip net.IP, err error) {
	// 存放网段中地址分配信息的数组
	ipam.Subnets = &map[string]string{}

	// 从文件中加载已经分配的网段信息
	err = ipam.load()
	if err != nil {
		log.Errorf("Error dump allocation info, %v", err)
	}

	// 返回网段的子网掩码的总长度和网段前面的固定位的长度
	_, subnet, _ = net.ParseCIDR(subnet.String())

	one, size := subnet.Mask.Size()

	if _, exist := (*ipam.Subnets)[subnet.String()]; !exist {
		// 用“0”填满这个网段的配置，1 << uint8(size - one)表示这个网段中有多少个可用地址
		// “size - one”是子网掩码后面的网络位数，2^(size - one)表示网段中的可用 IP 数
		// 2^(size - one)等价于1 << uint8(size - one)
		(*ipam.Subnets)[subnet.String()] = strings.Repeat("0", 1<<uint8(size-one))
	}

	// 遍历网段的位图数组
	for c := range (*ipam.Subnets)[subnet.String()] {
		// 找到数组中为”0“的项和数组序号，即可以分配的 IP
		if (*ipam.Subnets)[subnet.String()][c] == '0' {
			// 设置这个为”0“的序号值为”1“，即分配这个 IP，这里需先转换成 byte 数组，修改后再转换成字符串赋值
			ipalloc := []byte((*ipam.Subnets)[subnet.String()])
			ipalloc[c] = '1'
			(*ipam.Subnets)[subnet.String()] = string(ipalloc)
			// 这里的 IP 为初始 IP，比如对于网段 192.168.0.0/16，这里就是 192.168.0.0
			ip = subnet.IP

			for t := uint(4); t > 0; t -= 1 {
				[]byte(ip)[4-t] += uint8(c >> ((t - 1) * 8))
			}
			// 由于此处 IP 是从1开始分配的，所以最后再加1
			ip[3] += 1
			break
		}
	}

	ipam.dump()
	return
}

func (ipam *IPAM) Release(subnet *net.IPNet, ipaddr *net.IP) error {
	ipam.Subnets = &map[string]string{}

	// _, subnet, _ = net.ParseCIDR(subnet.String())

	err := ipam.load()
	if err != nil {
		log.Errorf("Error dump allocation info, %v", err)
	}

	// 计算 IP 地址在网段位图数组中的索引位置
	c := 0
	// 将 IP 地址转换成4个字节的表达方式
	releaseIP := ipaddr.To4()
	// 由于 IP 是从1开始分配的，所以转换成索引应减1
	releaseIP[3] -= 1
	for t := uint(4); t > 0; t -= 1 {
		// 与分配 IP 相反，释放 IP 获得索引的方式是 IP 地址的每一位相减之后分别左移将对应的数值加到索引上
		c += int(releaseIP[t-1]-subnet.IP[t-1]) << ((4 - t) * 8)
	}

	ipalloc := []byte((*ipam.Subnets)[subnet.String()])
	ipalloc[c] = '0'
	(*ipam.Subnets)[subnet.String()] = string(ipalloc)

	ipam.dump()
	return nil
}

// 加载网段地址分配信息
func (ipam *IPAM) load() error {
	if _, err := os.Stat(ipam.SubnetAllocatorPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		} else {
			return err
		}
	}
	subnetConfigFile, err := os.Open(ipam.SubnetAllocatorPath)
	defer func() {
		subnetConfigFile.Close()
	}()
	if err != nil {
		return err
	}
	subnetJson := make([]byte, 2000)
	n, err := subnetConfigFile.Read(subnetJson)
	if err != nil {
		return err
	}

	err = json.Unmarshal(subnetJson[:n], ipam.Subnets)
	if err != nil {
		log.Errorf("Error dump allocation info, %v", err)
		return err
	}
	return nil
}

// 存储网段地址分配信息
func (ipam *IPAM) dump() error {
	ipamConfigFileDir, _ := path.Split(ipam.SubnetAllocatorPath)
	if _, err := os.Stat(ipamConfigFileDir); err != nil {
		if os.IsNotExist(err) {
			os.MkdirAll(ipamConfigFileDir, 0644)
		} else {
			return err
		}
	}
	subnetConfigFile, err := os.OpenFile(ipam.SubnetAllocatorPath, os.O_TRUNC|os.O_WRONLY|os.O_CREATE, 0644)
	defer func() {
		subnetConfigFile.Close()
	}()
	if err != nil {
		return err
	}

	ipamConfigJson, err := json.Marshal(ipam.Subnets)
	if err != nil {
		return err
	}

	_, err = subnetConfigFile.Write(ipamConfigJson)
	if err != nil {
		return err
	}

	return nil
}

func Connect(networkName string, cinfo *container.ContainerInfo) error {
	// 从 networks 字典中取到容器连接的网络的信息，networks 字典中保存了当前已经创建的网络
	network, ok := networks[networkName]
	if !ok {
		return fmt.Errorf("no such Network: %s", networkName)
	}
	// 通过调用 IPAM 从网络的网段中获取可用的 IP 作为容器 IP 地址
	ip, err := ipAllocator.Allocate(network.IpRange)
	if err != nil {
		return err
	}
	// 创建网络端点
	ep := &Endpoint{
		ID:          fmt.Sprintf("%s-%s", cinfo.Id, networkName),
		IPAddress:   ip,
		Network:     network,
		PortMapping: cinfo.PortMapping,
	}
	// 调用网络驱动的 connect 方法连接和配置网络端点
	if err = drivers[network.Driver].Connect(network, ep); err != nil {
		return err
	}
	// 到容器的namespace中配置容器网络、设备IP地址和路由信息
	if err = configEndpointIpAddressAndRoute(ep, cinfo); err != nil {
		return err
	}
	// 配置容器到宿主机的端口映射，例如 mydocker run -p 8080:80
	return configPortMapping(ep, cinfo)
}

func configEndpointIpAddressAndRoute(ep *Endpoint, cinfo *container.ContainerInfo) error {
	// 通过网络端点中“Veth”的另一端
	peerLink, err := netlink.LinkByName(ep.Device.PeerName)
	if err != nil {
		return fmt.Errorf("fail config endpoint: %v", err)
	}

	// 将容器的网络端点加入到容器的网络空间中
	defer enterContainerNetns(&peerLink, cinfo)()

	// 获取到容器的 IP 地址及网段，用于配置容器内部接口地址
	// 192.168.1.2,192.168.1.0/24 => 192.168.1.2/24
	interfaceIP := *ep.Network.IpRange
	interfaceIP.IP = ep.IPAddress

	// 设置容器内 Veth 端点的 IP
	if err = setInterfaceIP(ep.Device.PeerName, interfaceIP.String()); err != nil {
		return fmt.Errorf("%v,%s", ep.Network, err)
	}

	// 启动容器内的 Veth 端点
	if err = setInterfaceUP(ep.Device.PeerName); err != nil {
		return err
	}

	// Net Namespace 中默认本地地址 127.0.0.1 的“lo”网卡是关闭状态的
	// 启动它以保证容器访问自己的请求
	if err = setInterfaceUP("lo"); err != nil {
		return err
	}

	// 0.0.0.0/0网段表示所有的 IP 地址段
	_, cidr, _ := net.ParseCIDR("0.0.0.0/0")

	// 构建要添加的路由数据，包括网络设备、网关 IP 及目的网段
	// 相关于 route add -net 0.0.0.0/0 gw {Bridge网桥地址} dev {容器内的Veth端点设备}
	defaultRoute := &netlink.Route{
		LinkIndex: peerLink.Attrs().Index,
		Gw:        ep.Network.IpRange.IP,
		Dst:       cidr,
	}

	if err = netlink.RouteAdd(defaultRoute); err != nil {
		return err
	}

	return nil
}

// 配置端口映射
func configPortMapping(ep *Endpoint, cinfo *container.ContainerInfo) error {
	for _, pm := range ep.PortMapping {
		portMapping := strings.Split(pm, ":")
		if len(portMapping) != 2 {
			log.Errorf("port mapping format error, %v", pm)
			continue
		}
		iptablesCmd := fmt.Sprintf("-t nat -A PREROUTING -p tcp -m tcp --dport %s -j DNAT --to-destination %s:%s",
			portMapping[0], ep.IPAddress.String(), portMapping[1])
		cmd := exec.Command("iptables", strings.Split(iptablesCmd, " ")...)
		output, err := cmd.Output()
		if err != nil {
			log.Errorf("iptables Output, %v", output)
			continue
		}
	}
	return nil
}
