package command

import (
	"errors"
	"fmt"
	"mydocker/pkg/cgroups/subsystems"
	"mydocker/pkg/container"
	"mydocker/pkg/network"
	"os"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

const (
	Usage = `Mydocker is a simple container runtime implementation. 
	         The purpose of this project is to learn how docker works and how towrite a docker by ourselves. 
	         Enjoy it, just for fun.`
)

var InitCommand = cli.Command{
	Name:  "init",
	Usage: `Init container process run user's process in container. Do not call it outside.`,
	Action: func(ctx *cli.Context) error {
		log.Infof("init come on")
		err := container.RunContainerInitProcess()
		return err
	},
}

var RunCommand = cli.Command{
	Name:  "run",
	Usage: `Create a container with namespace and cgroups limit mydocker run -it [command]`,
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:  "ti",
			Usage: "enable tty",
		},
		cli.StringFlag{
			Name:  "name",
			Usage: "container name",
		},
		cli.BoolFlag{
			Name:  "d",
			Usage: "detach container",
		},
		cli.StringFlag{
			Name:  "m",
			Usage: "memory limit",
		},
		cli.StringFlag{
			Name:  "cpushare",
			Usage: "cpushare limit",
		},
		cli.StringFlag{
			Name:  "cpuset",
			Usage: "cpuset limit",
		},
		cli.StringFlag{
			Name:  "v",
			Usage: "volume",
		},
		cli.StringSliceFlag{
			Name:  "e",
			Usage: "set environment",
		},
	},

	Action: func(ctx *cli.Context) error {
		if len(ctx.Args()) < 1 {
			return errors.New("missing container command")
		}
		var cmdArray []string
		for _, arg := range ctx.Args() {
			cmdArray = append(cmdArray, arg)
		}
		imageName := cmdArray[0]
		cmdArray = cmdArray[1:]

		createTty := ctx.Bool("ti")
		detach := ctx.Bool("d")
		if createTty && detach {
			return fmt.Errorf("ti and d paramter can not both provided")
		}

		resConf := &subsystems.ResourceConfig{
			MemoryLimit: ctx.String("m"),
			CpuSet:      ctx.String("cpuset"),
			CpuShare:    ctx.String("cpushare"),
		}
		log.Infof("createTty %v", createTty)

		containerName := ctx.String("name")
		volume := ctx.String("v")
		network := ctx.String("net")
		envSlice := ctx.StringSlice("e")
		portmapping := ctx.StringSlice("p")
		run(createTty, cmdArray, resConf, volume, containerName, imageName, envSlice, network, portmapping)
		return nil
	},
}

var ListCommand = cli.Command{
	Name:  "ps",
	Usage: "list all the containers",
	Action: func(context *cli.Context) error {
		listContainers()
		return nil
	},
}

var CommitCommand = cli.Command{
	Name:  "commit",
	Usage: "commit a container into image",
	Action: func(ctx *cli.Context) error {
		if len(ctx.Args()) < 2 {
			return errors.New("missing container name and image name")
		}
		containerName := ctx.Args().Get(0)
		imageName := ctx.Args().Get(1)
		commitContainer(containerName, imageName)
		return nil
	},
}

var LogCommand = cli.Command{
	Name:  "logs",
	Usage: "print logs of a container",
	Action: func(ctx *cli.Context) error {
		if len(ctx.Args()) < 1 {
			return fmt.Errorf("please input your container name")
		}
		containerName := ctx.Args().Get(0)
		logContainer(containerName)
		return nil
	},
}

var ExecCommand = cli.Command{
	Name:  "exec",
	Usage: "exec a command into container",
	Action: func(ctx *cli.Context) error {
		//This is for callback
		if os.Getenv(ENV_EXEC_PID) != "" {
			log.Infof("pid callback pid %s", os.Getgid())
			return nil
		}

		if len(ctx.Args()) < 2 {
			return fmt.Errorf("missing container name or command")
		}
		containerName := ctx.Args().Get(0)
		var commandArray []string
		commandArray = append(commandArray, ctx.Args().Tail()...)
		execContainer(containerName, commandArray)
		return nil
	},
}

var StopCommand = cli.Command{
	Name:  "stop",
	Usage: "stop a container",
	Action: func(ctx *cli.Context) error {
		if len(ctx.Args()) < 1 {
			return fmt.Errorf("missing container name")
		}
		containerName := ctx.Args().Get(0)
		stopContainer(containerName)
		return nil
	},
}

var RemoveCommand = cli.Command{
	Name:  "rm",
	Usage: "remove unused containers",
	Action: func(context *cli.Context) error {
		if len(context.Args()) < 1 {
			return fmt.Errorf("missing container name")
		}
		containerName := context.Args().Get(0)
		removeContainer(containerName)
		return nil
	},
}

var NetworkCommand = cli.Command{
	Name:  "network",
	Usage: "container network commands",
	Subcommands: []cli.Command{
		{
			Name:  "create",
			Usage: "create a container network",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "driver",
					Usage: "network driver",
				},
				cli.StringFlag{
					Name:  "subnet",
					Usage: "subnet cidr",
				},
			},
			Action: func(ctx *cli.Context) error {
				if len(ctx.Args()) < 1 {
					return fmt.Errorf("missing network name")
				}
				network.Init()
				err := network.CreateNetwork(ctx.String("driver"), ctx.String("subnet"), ctx.Args()[0])
				if err != nil {
					return fmt.Errorf("create network error: %+v", err)
				}
				return nil
			},
		},
		{
			Name:  "list",
			Usage: "list container network",
			Action: func(ctx *cli.Context) error {
				network.Init()
				network.ListNetwork()
				return nil
			},
		},
		{
			Name:  "remove",
			Usage: "remove container network",
			Action: func(ctx *cli.Context) error {
				if len(ctx.Args()) < 1 {
					return fmt.Errorf("missing network name")
				}
				network.Init()
				if err := network.DeleteNetwork(ctx.Args()[0]); err != nil {
					return fmt.Errorf("remove network error: %+v", err)
				}

				return nil
			},
		},
	},
}
