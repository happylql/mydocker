package cgroups

import (
	"mydocker/pkg/cgroups/subsystems"

	log "github.com/sirupsen/logrus"
)

type CgroupManager struct {
	Path     string
	Resource *subsystems.ResourceConfig
}

func NewCgroupManager(path string) *CgroupManager {
	return &CgroupManager{
		Path: path,
	}
}

func (c *CgroupManager) ApplyAll(pid int) error {
	for _, subSysIns := range subsystems.SubsystemIns {
		if err := subSysIns.Apply(c.Path, pid); err != nil {
			log.Warnf("apply cgroup fail %v", err)
		}
	}
	return nil
}

func (c *CgroupManager) SetAll(res *subsystems.ResourceConfig) error {
	for _, subSysIns := range subsystems.SubsystemIns {
		if err := subSysIns.Set(c.Path, res); err != nil {
			log.Warnf("set cgroup fail %v", err)
		}
	}
	return nil
}

func (c *CgroupManager) RemoveAll() error {
	for _, subSysIns := range subsystems.SubsystemIns {
		if err := subSysIns.Remove(c.Path); err != nil {
			log.Warnf("remove cgroup fail %v", err)
		}
	}
	return nil
}
