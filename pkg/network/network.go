package network

import (
	"encoding/json"
	"fmt"
	"mydocker/pkg/container"
	"net"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"text/tabwriter"

	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const defaultNetworkPath = "/var/run/mydocker/network/network/"

var (
	drivers  = map[string]NetworkDriver{}
	networks = map[string]*Network{}
)

// 网络端点
type Endpoint struct {
	ID          string           `json:"id"`
	Device      netlink.Veth     `json:"dev"`
	IPAddress   net.IP           `json:"ip"`
	MacAddress  net.HardwareAddr `json:"mac"`
	Network     *Network
	PortMapping []string
}

// 网络
type Network struct {
	Name    string
	IpRange *net.IPNet
	Driver  string
}

// 网络驱动
type NetworkDriver interface {
	Name() string
	Create(subnet string, name string) (*Network, error)
	Delete(network Network) error
	Connect(network *Network, endpoint *Endpoint) error
	Disconnect(network Network, endpoint *Endpoint) error
}

func Init() error {
	var bridgeDriver = BridgeNetworkDriver{}
	drivers[bridgeDriver.Name()] = &bridgeDriver

	// 判断网络的配置目录是否存在，不存在则创建
	if _, err := os.Stat(defaultNetworkPath); err != nil {
		if os.IsNotExist(err) {
			os.MkdirAll(defaultNetworkPath, 0644)
		} else {
			return err
		}
	}

	// 检查网络配置目录中的所有文件，walk会遍历指定的 path 目录，并执行第二个参数中的函数指针去处理目录下的每一个文件
	filepath.Walk(defaultNetworkPath, func(nwPath string, info os.FileInfo, err error) error {
		// 如果是目录则跳过
		if info.IsDir() {
			return nil
		}
		// 加载文件名作为网络名
		_, nwName := path.Split(nwPath)
		nw := &Network{
			Name: nwName,
		}
		// 加载网络的配置信息
		if err := nw.load(nwPath); err != nil {
			log.Errorf("error load network: %s", err)
		}
		// 将网络的配置信息加入到 networks 字典中
		networks[nwName] = nw
		return nil
	})

	log.Infof("networks: %v", networks)

	return nil
}

func ListNetwork() {
	w := tabwriter.NewWriter(os.Stdout, 12, 1, 3, ' ', 0)
	fmt.Fprint(w, "NAME\tIpRange\tDriver\n")
	for _, nw := range networks {
		fmt.Fprintf(w, "%s\t%s\t%s\n",
			nw.Name,
			nw.IpRange.String(),
			nw.Driver,
		)
	}
	if err := w.Flush(); err != nil {
		log.Errorf("Flush error %v", err)
		return
	}
}

func CreateNetwork(driver, subnet, name string) error {
	// ParseCIDR 将网段的字符串转换成 net.IPNet 的对象
	_, cidr, _ := net.ParseCIDR(subnet)
	// 通过 IPAM 分配网关 IP，获取到网段中第一个 IP 作为网关的 IP
	gatewayIp, err := ipAllocator.Allocate(cidr)
	if err != nil {
		return err
	}
	cidr.IP = gatewayIp

	// 调用指定的网络驱动创建网络
	nw, err := drivers[driver].Create(cidr.String(), name)
	if err != nil {
		return err
	}

	// 保存网络信息，将网络的信息保存在文件系统中，以便查询和在网络上连接网络端点
	return nw.dump(defaultNetworkPath)
}

func DeleteNetwork(networkName string) error {
	nw, ok := networks[networkName]
	if !ok {
		return fmt.Errorf("no Such Network: %s", networkName)
	}

	if err := ipAllocator.Release(nw.IpRange, &nw.IpRange.IP); err != nil {
		return fmt.Errorf("error Remove Network gateway ip: %s", err)
	}

	if err := drivers[nw.Driver].Delete(*nw); err != nil {
		return fmt.Errorf("error Remove Network DriverError: %s", err)
	}

	return nw.remove(defaultNetworkPath)
}

func (nw *Network) dump(dumpPath string) error {
	if _, err := os.Stat(dumpPath); err != nil {
		if os.IsNotExist(err) {
			os.MkdirAll(dumpPath, 0644)
		} else {
			return err
		}
	}

	nwPath := path.Join(dumpPath, nw.Name)
	// 打开保存的文件用于写入，模式参数为存在内容则清空、只写入、不存在则创建
	nwFile, err := os.OpenFile(nwPath, os.O_TRUNC|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		log.Errorf("error：", err)
		return err
	}
	defer nwFile.Close()

	nwJson, err := json.Marshal(nw)
	if err != nil {
		log.Errorf("error：", err)
		return err
	}

	_, err = nwFile.Write(nwJson)
	if err != nil {
		log.Errorf("error：", err)
		return err
	}
	return nil
}

func (nw *Network) load(dumpPath string) error {
	nwConfigFile, err := os.Open(dumpPath)
	defer func() {
		nwConfigFile.Close()
	}()
	if err != nil {
		return err
	}
	nwJson := make([]byte, 2000)
	n, err := nwConfigFile.Read(nwJson)
	if err != nil {
		return err
	}

	err = json.Unmarshal(nwJson[:n], nw)
	if err != nil {
		log.Errorf("Error load nw info", err)
		return err
	}
	return nil
}

func (nw *Network) remove(dumpPath string) error {
	if _, err := os.Stat(path.Join(dumpPath, nw.Name)); err != nil {
		if os.IsNotExist(err) {
			return nil
		} else {
			return err
		}
	} else {
		return os.Remove(path.Join(dumpPath, nw.Name))
	}
}

// 将容器的网络端点加入到容器的网络空间中
// 并锁定当前程序所执行的线程，使当前线程进入到容器的网络空间
// 返回值是一个函数指针，执行这个返回函数才会退出容器的网络空间，回归到宿主机的网络空间
func enterContainerNetns(enLink *netlink.Link, cinfo *container.ContainerInfo) func() {
	// 获取容器的 Net Namespace
	f, err := os.OpenFile(fmt.Sprintf("/proc/%s/ns/net", cinfo.Pid), os.O_RDONLY, 0)
	if err != nil {
		log.Errorf("error get container net namespace, %v", err)
	}

	// 取到文件的文件描述符
	nsFD := f.Fd()
	// 锁定当前程序所执行的线程，如果不锁定操作系统线程的话，GO语言的goroutine可能会被调度到别的线程上去，
	// 就不能保证一直在所需要的网络空间中，所以调用 runtime.LockOSThread 锁定当前程序执行的线程
	runtime.LockOSThread()

	// 修改 Veth的另外一端，将其移到容器的 Net Namespace中
	if err = netlink.LinkSetNsFd(*enLink, int(nsFD)); err != nil {
		log.Errorf("error set link netns , %v", err)
	}

	// 获取当前的 Net Namespace
	origns, err := netns.Get()
	if err != nil {
		log.Errorf("error get current netns, %v", err)
	}

	// 设置当前进程到新的 Net Namespace，并在函数执行完成之后再恢复到之前的 Net Namespace
	if err = netns.Set(netns.NsHandle(nsFD)); err != nil {
		log.Errorf("error set netns, %v", err)
	}
	return func() {
		// 恢复到上面获取到的之前的 Net Namespace
		netns.Set(origns)
		// 关闭 Namespace 文件
		origns.Close()
		// 取消对当前程序的线程锁定
		runtime.UnlockOSThread()
		// 关闭 Namespace 文件
		f.Close()
	}
}
