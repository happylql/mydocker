package network

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

type BridgeNetworkDriver struct {
}

func (d *BridgeNetworkDriver) Name() string {
	return "bridge"
}

func (d *BridgeNetworkDriver) Create(subnet, name string) (*Network, error) {
	ip, ipRange, _ := net.ParseCIDR(subnet)
	ipRange.IP = ip
	n := &Network{
		Name:    name,
		IpRange: ipRange,
		Driver:  d.Name(),
	}
	err := d.initBridge(n)
	if err != nil {
		log.Errorf("error init bridge: %v", err)
	}

	return n, err
}

func (d *BridgeNetworkDriver) initBridge(n *Network) error {
	// 创建 Bridge 虚拟设备
	bridgeName := n.Name
	if err := createBridgeInterface(bridgeName); err != nil {
		return fmt.Errorf("error add bridge： %s, Error: %v", bridgeName, err)
	}

	// 设置 Bridge 设备的地址和路由
	gatewayIP := *n.IpRange
	gatewayIP.IP = n.IpRange.IP

	if err := setInterfaceIP(bridgeName, gatewayIP.String()); err != nil {
		return fmt.Errorf("error assigning address: %s on bridge: %s with an error of: %v", gatewayIP, bridgeName, err)
	}

	// 启动 Bridge 设备
	if err := setInterfaceUP(bridgeName); err != nil {
		return fmt.Errorf("error set bridge up: %s, Error: %v", bridgeName, err)
	}

	// 设置 iptabels 的 SNAT 规则
	if err := setupIPTables(bridgeName, n.IpRange); err != nil {
		return fmt.Errorf("error setting iptables for %s: %v", bridgeName, err)
	}

	return nil
}

func (d *BridgeNetworkDriver) deleteBridge(n *Network) error {
	bridgeName := n.Name

	// get the link
	l, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return fmt.Errorf("getting link with name %s failed: %v", bridgeName, err)
	}

	// delete the link
	if err := netlink.LinkDel(l); err != nil {
		return fmt.Errorf("failed to remove bridge interface %s delete: %v", bridgeName, err)
	}

	return nil
}

// 连接一个网络和网络端点
func (d *BridgeNetworkDriver) Connect(network *Network, endpoint *Endpoint) error {
	// 获取网络名，即 Linux Bridge 的接口名
	bridgeName := network.Name
	// 获取接口的对象和接口属性
	br, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return err
	}

	// 创建 Veth 接口的配置
	la := netlink.NewLinkAttrs()
	// 由于 Linux 接口名的限制，名字取 endpoint ID 的前5位
	la.Name = endpoint.ID[:5]
	// 通过设置 Veth 接口的 master 属性，设置这个 Veth 的一端挂载到网络对应的 Linux Bridge 上
	la.MasterIndex = br.Attrs().Index

	// 创建 Veth 对象，通过 PeerName 配置 Veth 另外一端的接口名
	// 配置 Veth 另外一端的名字 cif-(endpoint ID 的前5位)
	endpoint.Device = netlink.Veth{
		LinkAttrs: la,
		PeerName:  "cif-" + endpoint.ID[:5],
	}

	if err = netlink.LinkAdd(&endpoint.Device); err != nil {
		return fmt.Errorf("error Add Endpoint Device: %v", err)
	}

	if err = netlink.LinkSetUp(&endpoint.Device); err != nil {
		return fmt.Errorf("error Add Endpoint Device: %v", err)
	}
	return nil
}

func (d *BridgeNetworkDriver) Disconnect(network Network, endpoint *Endpoint) error {
	return nil
}

func (d *BridgeNetworkDriver) Delete(network Network) error {
	bridgeName := network.Name
	br, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return err
	}
	return netlink.LinkDel(br)
}

// 创建 Linux bridge 设备
func createBridgeInterface(bridgeName string) error {
	// 先检查是否已经存在了这个同名的 Bridge 设备
	_, err := net.InterfaceByName(bridgeName)
	// 如果已经存在或者不是报不存在的错误则返回错误
	if err == nil || !strings.Contains(err.Error(), "no such network interface") {
		return fmt.Errorf("network interface %s is existed", bridgeName)
	}

	// 初始化一个 netlink 的 Link 基础对象
	la := netlink.NewLinkAttrs()
	la.Name = bridgeName

	br := &netlink.Bridge{LinkAttrs: la}
	// 创建 Bridge 虚拟网络设备
	if err := netlink.LinkAdd(br); err != nil {
		return fmt.Errorf("bridge creation failed for bridge %s: %v", bridgeName, err)
	}
	return nil
}

// 设置网络接口为 UP 状态
func setInterfaceUP(interfaceName string) error {
	iface, err := netlink.LinkByName(interfaceName)
	if err != nil {
		return fmt.Errorf("error retrieving a link named [ %s ]: %v", iface.Attrs().Name, err)
	}

	// 设置接口状态为“UP”状态
	if err := netlink.LinkSetUp(iface); err != nil {
		return fmt.Errorf("error enabling interface for %s: %v", interfaceName, err)
	}
	return nil
}

// 设置一个网络接口的 IP 地址
func setInterfaceIP(name string, rawIP string) error {
	retries := 2
	var iface netlink.Link
	var err error
	for i := 0; i < retries; i++ {
		// 找到需要设置的网络接口
		iface, err = netlink.LinkByName(name)
		if err == nil {
			break
		}
		log.Debugf("error retrieving new bridge netlink link [ %s ]... retrying", name)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		return fmt.Errorf("abandoning retrieving the new bridge link from netlink, Run [ ip link ] to troubleshoot the error: %v", err)
	}
	ipNet, err := netlink.ParseIPNet(rawIP)
	if err != nil {
		return err
	}
	addr := &netlink.Addr{IPNet: ipNet, Peer: ipNet, Label: "", Flags: 0, Scope: 0, Broadcast: nil}
	// 给网络接口配置地址，相当于 ip addr add xxx 的命令
	return netlink.AddrAdd(iface, addr)
}

// 设置 iptables 对应 bridge 的 MASQUERADE 规则
func setupIPTables(bridgeName string, subnet *net.IPNet) error {
	// GO 没有直接操作 iptables 的库，只能通过命令的方式来调用
	iptablesCmd := fmt.Sprintf("-t nat -A POSTROUTING -s %s ! -o %s -j MASQUERADE", subnet.String(), bridgeName)
	// 执行 iptables 命令配置 SNAT 规则
	cmd := exec.Command("iptables", strings.Split(iptablesCmd, " ")...)
	output, err := cmd.Output()
	if err != nil {
		log.Errorf("iptables Output, %v", output)
	}
	return nil
}
