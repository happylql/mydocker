package main

import (
	"mydocker/pkg/command"
	"os"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

func main() {
	app := cli.NewApp()
	app.Name = "mydocker"
	app.Usage = command.Usage

	app.Commands = []cli.Command{
		command.InitCommand,
		command.RunCommand,
		command.ListCommand,
		command.CommitCommand,
		command.LogCommand,
		command.ExecCommand,
		command.StopCommand,
		command.RemoveCommand,
		command.NetworkCommand,
	}
	app.Before = func(c *cli.Context) error {
		// 替换默认的ASCII格式化
		log.SetFormatter(&log.JSONFormatter{})
		log.SetOutput(os.Stdout)
		return nil
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
