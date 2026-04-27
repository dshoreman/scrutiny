package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/analogj/scrutiny/webapp/backend/pkg/config"
	"github.com/analogj/scrutiny/webapp/backend/pkg/database"
	"github.com/analogj/scrutiny/webapp/backend/pkg/errors"
	"github.com/analogj/scrutiny/webapp/backend/pkg/version"
	"github.com/analogj/scrutiny/webapp/backend/pkg/web"
	"github.com/sirupsen/logrus"

	utils "github.com/analogj/go-util/utils"
	"github.com/fatih/color"
	"github.com/urfave/cli/v2"
)

var goos string
var goarch string

func main() {

	config, err := config.Create()
	if err != nil {
		fmt.Printf("FATAL: %+v\n", err)
		os.Exit(1)
	}

	configFilePath := "/opt/scrutiny/config/scrutiny.yaml"
	configFilePathAlternative := "/opt/scrutiny/config/scrutiny.yml"
	if !utils.FileExists(configFilePath) && utils.FileExists(configFilePathAlternative) {
		configFilePath = configFilePathAlternative
	}

	err = config.ReadConfig(configFilePath)
	// Exit if there are errors with the default config, unless it doesn't exist
	if _, missing := err.(errors.ConfigFileMissingError); err != nil && !missing {
		log.Print(color.HiRedString("CONFIG ERROR: %v", err))
		os.Exit(1)
	}

	cli.CommandHelpTemplate = `NAME:
   {{.HelpName}} - {{.Usage}}
USAGE:
   {{if .UsageText}}{{.UsageText}}{{else}}{{.HelpName}} {{if .ArgsUsage}}{{.ArgsUsage}}{{else}}[arguments...]{{end}}{{end}}{{if .Category}}
CATEGORY:
   {{.Category}}{{end}}{{if .Description}}
DESCRIPTION:
   {{.Description}}{{end}}{{if .VisibleFlags}}
OPTIONS:
   {{range .VisibleFlags}}{{.}}
   {{end}}{{end}}
`

	flags := map[string]cli.Flag{
		"config": &cli.StringFlag{
			Name:  "config",
			Aliases: []string{"C"},
			Usage: "Specify the path to the config file",
			Action: func(c *cli.Context, filePath string) error {
				if filePath != "" {
					if err := config.ReadConfig(filePath); err != nil {
						return err
					}
				}
				return nil
			},
		},
	}

	app := &cli.App{
		Name:     "scrutiny",
		Usage:    "WebUI for smartd S.M.A.R.T monitoring",
		Version:  version.VERSION,
		Compiled: time.Now(),
		Authors: []*cli.Author{
			{
				Name:  "Jason Kulatunga",
				Email: "jason@thesparktree.com",
			},
		},
		Before: func(c *cli.Context) error {
			scrutiny := "github.com/AnalogJ/scrutiny"

			var versionInfo string
			if len(goos) > 0 && len(goarch) > 0 {
				versionInfo = fmt.Sprintf("%s.%s-%s", goos, goarch, version.VERSION)
			} else {
				versionInfo = fmt.Sprintf("dev-%s", version.VERSION)
			}

			subtitle := scrutiny + utils.LeftPad2Len(versionInfo, " ", 65-len(scrutiny))

			color.New(color.FgGreen).Fprintf(c.App.Writer, utils.StripIndent(
				`
			 ___   ___  ____  __  __  ____  ____  _  _  _  _
			/ __) / __)(  _ \(  )(  )(_  _)(_  _)( \( )( \/ )
			\__ \( (__  )   / )(__)(   )(   _)(_  )  (  \  /
			(___/ \___)(_)\_)(______) (__) (____)(_)\_) (__)
			%s

			`), subtitle)

			return nil
		},
		Flags: []cli.Flag{flags["config"]},
		Commands: []*cli.Command{
			{
				Name: "device",
				Usage: "Tools to manage disk metadata",
				Subcommands: []*cli.Command{{
					Name: "list",
					Usage: "Get information about all devices in Scrutiny",
					Action: func(c *cli.Context) error {
						logger, logFile, err := CreateLogger(config)
						if logFile != nil {
							defer logFile.Close()
						}
						if err != nil {
							return err
						}

						db, err := database.NewScrutinyRepository(config, logger)
						if err != nil {
							panic(err)
						}
						return deviceListAction(c, db)
					},

				}, {
					Name: "patch",
					Usage: "Scan for and/or patch metadata discrepencies",
					Action: func(c *cli.Context) error {
						logger, logFile, err := CreateLogger(config)
						if logFile != nil {
							defer logFile.Close()
						}
						if err != nil {
							return err
						}

						db, err := database.NewScrutinyRepository(config, logger)
						if err != nil {
							panic(err)
						}
						return devicePatchAction(c, db)
					},
				}},
			}, {
				Name:  "start",
				Usage: "Start the scrutiny server",
				Flags: []cli.Flag{
					flags["config"],
					&cli.BoolFlag{
						Name: "debug",
						EnvVars: []string{"SCRUTINY_DEBUG", "DEBUG"},
						Usage: "Enable debug logging",
						Action: func(c *cli.Context, enabled bool) error {
							if enabled {
								config.Set("log.level", "DEBUG")
							}
							return nil
						},
					},
					&cli.StringFlag{
						Name:"log-file",
						Usage:"Path to file for logging. Leave empty to use STDOUT",
						Value:"",
						EnvVars: []string{"SCRUTINY_LOG_FILE"},
						Action: func(c *cli.Context, filePath string) error {
							config.Set("log.file", filePath)
							return nil
						},
					},
				},
				Action: func(c *cli.Context) error {
					fmt.Fprintln(c.App.Writer, c.Command.Usage)
					webLogger, logFile, err := CreateLogger(config)
					if logFile != nil {
						defer logFile.Close()
					}
					if err != nil {
						return err
					}

					settingsData, err := json.Marshal(config.AllSettings())
					webLogger.Debug(string(settingsData), err)

					webServer := web.AppEngine{Config: config, Logger: webLogger}

					return webServer.Start()
				},
			},
		},
	}

	err = app.Run(os.Args)
	if err != nil {
		log.Fatal(color.HiRedString("ERROR: %v", err))
	}
}

func CreateLogger(appConfig config.Interface) (*logrus.Entry, *os.File, error) {
	logger := logrus.WithFields(logrus.Fields{
		"type": "web",
	})
	//set default log level
	if level, err := logrus.ParseLevel(appConfig.GetString("log.level")); err == nil {
		logger.Logger.SetLevel(level)
	} else {
		logger.Logger.SetLevel(logrus.InfoLevel)
	}

	var logFile *os.File
	var err error
	if appConfig.IsSet("log.file") && len(appConfig.GetString("log.file")) > 0 {
		logFile, err = os.OpenFile(appConfig.GetString("log.file"), os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			logger.Logger.Errorf("Failed to open log file %s for output: %s", appConfig.GetString("log.file"), err)
			return nil, logFile, err
		}
		logger.Logger.SetOutput(io.MultiWriter(os.Stderr, logFile))
	}
	return logger, logFile, nil
}
