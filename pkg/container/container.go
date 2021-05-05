package container

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	log "github.com/sirupsen/logrus"
)

var (
	RUNNING             string = "running"
	STOP                string = "stopped"
	EXIT                string = "exited"
	DefaultInfoLocation string = "/var/run/mydocker/%s/"
	ConfigName          string = "config.json"
	ContainerLogFile    string = "container.log"
	RootUrl             string = "/root"
	MntUrl              string = "/root/mnt/%s"
	WriteLayerUrl       string = "/root/writeLayer/%s"
)

type ContainerInfo struct {
	Pid         string   `json:"pid"` // 容器的init进程在宿主机上的PID
	Id          string   `json:"id"`
	Name        string   `json:"name"`
	Command     string   `json:"command"` // 容器内init进程的运行命令
	CreatedTime string   `json:"createTime"`
	Status      string   `json:"Status"`
	Volume      string   `json:"volume"`      //容器的数据卷
	PortMapping []string `json:"portmapping"` //端口映射
}

func NewParentProcess(tty bool, containerName, volume, imageName string, envSlice []string) (*exec.Cmd, *os.File) {
	readPipe, writePipe, err := NewPipe()
	if err != nil {
		log.Errorf("new pipe error %v", err)
		return nil, nil
	}
	initCmd, err := os.Readlink("/proc/self/exe")
	if err != nil {
		log.Errorf("get init process error %v", err)
		return nil, nil
	}

	// 自己调用自己，对创建的进程初始化
	cmd := exec.Command(initCmd, "init")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS |
			syscall.CLONE_NEWNET | syscall.CLONE_NEWIPC,
	}
	if tty {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	} else {
		dirURL := fmt.Sprintf(DefaultInfoLocation, containerName)
		if err := os.MkdirAll(dirURL, 0622); err != nil {
			log.Errorf("NewParentProcess mkdir %s error %v", dirURL, err)
			return nil, nil
		}
		stdLogFilePath := dirURL + ContainerLogFile
		stdLogFile, err := os.Create(stdLogFilePath)
		if err != nil {
			log.Errorf("NewParentProcess create file %s error %v", stdLogFilePath, err)
			return nil, nil
		}
		// 把生成好的文件赋值给stdout，这样就能把容器内的标准输出重定向到这个文件中
		cmd.Stdout = stdLogFile
	}

	// 注意，在这传入管道文件读取端的句柄
	cmd.ExtraFiles = []*os.File{readPipe}
	cmd.Env = append(os.Environ(), envSlice...)
	NewWorkSpace(volume, imageName, containerName)
	cmd.Dir = fmt.Sprintf(MntUrl, containerName)
	return cmd, writePipe
}

func NewPipe() (*os.File, *os.File, error) {
	read, write, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	return read, write, nil
}
