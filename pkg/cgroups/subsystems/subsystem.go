package subsystems

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"strings"
)

// 用于传递资源限制配置的结构体，包含内存限制、CPU时间片权重、CPU核心数
type ResourceConfig struct {
	MemoryLimit string
	CpuShare    string
	CpuSet      string
}

// 子系统接口，这里将cgroup抽象成了path，原因是cgroup在hierarchy的路径，便是虚拟文件系统中的虚拟路径
type Subsystem interface {
	Name() string
	// 设置某个cgroup在这个subsystem中的资源限制
	Set(path string, res *ResourceConfig) error
	// 将进程添加到某个cgroup中
	Apply(path string, pid int) error
	// 移除某个cgroup
	Remove(path string) error
}

// 通过不同的subsystem初始化实例创建资源限制处理链数组
var (
	SubsystemIns = []Subsystem{
		&CpusetSubsystem{},
		&MemorySubsystem{},
		&CpuSubSystem{},
	}
)

// 得到cgroup在文件系统中的绝对路径
func GetCgroupPath(subsystem, cgroupPath string, autoCreate bool) (string, error) {
	cgroupRoot := FindCgroupMountpoint(subsystem)
	if _, err := os.Stat(path.Join(cgroupRoot, cgroupPath)); err == nil || (autoCreate && os.IsNotExist(err)) {
		if os.IsNotExist(err) {
			if err := os.Mkdir(path.Join(cgroupRoot, cgroupPath), 0755); err != nil {
				return "", fmt.Errorf("error create cgroup %v", err)
			}
		}
		return path.Join(cgroupRoot, cgroupPath), nil
	} else {
		return "", fmt.Errorf("cgroup path error %v", err)
	}
}

// 找出挂载了某个subsystem的cgroup的根节点目录
// 例如：44 33 0:38 / /sys/fs/cgroup/memory rw,nosuid,nodev,noexec,relatime shared:21 - cgroup cgroup rw,memory
func FindCgroupMountpoint(subsystem string) string {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		txt := scanner.Text()
		fields := strings.Split(txt, " ")
		for _, opt := range strings.Split(fields[len(fields)-1], ",") {
			if opt == subsystem {
				return fields[4]
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return ""
	}
	return ""
}
