package command

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"mydocker/pkg/container"
	"strconv"
	"syscall"

	log "github.com/sirupsen/logrus"
)

func stopContainer(containerName string) {
	// 根据容器名获取对应的主进程PID
	pid, err := getContainerPidByName(containerName)
	if err != nil {
		log.Errorf("Get container pid by name %s error %v", containerName, err)
		return
	}
	// 将string类型的PID转换为int类型
	pidInt, err := strconv.Atoi(pid)
	if err != nil {
		log.Errorf("Conver pid from string to int error %v", err)
		return
	}
	// 系统调用kill可以发送信号给进程，通过传递syscall.SIGTERM信号，去杀掉容器主进程
	if err := syscall.Kill(pidInt, syscall.SIGTERM); err != nil {
		log.Errorf("Stop container %s error %v", containerName, err)
		return
	}
	// 根据容器名获取对应的信息对象
	containerInfo, err := getContainerInfoByName(containerName)
	if err != nil {
		log.Errorf("Get container %s info error %v", containerName, err)
		return
	}
	// 至此，容器进程已经被kill，所以下面需要修改容器状态，PID可以置为空
	containerInfo.Status = container.STOP
	containerInfo.Pid = " "
	// 将修改后的信息序列化成json的字符串
	newContentBytes, err := json.Marshal(containerInfo)
	if err != nil {
		log.Error("Json marshal %s error %v", containerName, err)
		return
	}
	dirURL := fmt.Sprintf(container.DefaultInfoLocation, containerName)
	configFilePath := dirURL + container.ConfigName
	// 重新写入新的数据覆盖原来的信息
	if err := ioutil.WriteFile(configFilePath, newContentBytes, 0622); err != nil {
		log.Errorf("Write file %s error", configFilePath, err)
	}
}

func getContainerInfoByName(containerName string) (*container.ContainerInfo, error) {
	// 构造存放容器信息的路径
	dirURL := fmt.Sprintf(container.DefaultInfoLocation, containerName)
	configFilePath := dirURL + container.ConfigName
	contentBytes, err := ioutil.ReadFile(configFilePath)
	if err != nil {
		log.Errorf("Read file %s error %v", configFilePath, err)
		return nil, err
	}
	var containerInfo container.ContainerInfo
	// 将容器信息字符串反序列化成对应的对象
	if err := json.Unmarshal(contentBytes, &containerInfo); err != nil {
		log.Error("GetContainerInfoByName unmarshal error %v", err)
		return nil, err
	}
	return &containerInfo, nil
}
