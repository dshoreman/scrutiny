package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	_ "go.uber.org/automaxprocs"

	utils "github.com/analogj/go-util/utils"
	"github.com/analogj/scrutiny/webapp/backend/pkg/config"
	"github.com/analogj/scrutiny/webapp/backend/pkg/database"
	"github.com/analogj/scrutiny/webapp/backend/pkg/errors"
	"github.com/analogj/scrutiny/webapp/backend/pkg/version"
	"github.com/analogj/scrutiny/webapp/backend/pkg/web"
	"github.com/fatih/color"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

var goos string
var goarch string

func main() {

	// Create a bootstrap logger early so all startup errors use structured logging
	bootstrapLogger := logrus.WithFields(logrus.Fields{"type": "web"})
	bootstrapLogger.Logger.SetLevel(logrus.InfoLevel)

	config, err := config.Create()
	if err != nil {
		bootstrapLogger.Fatalf("FATAL: %+v", err)
	}

	configFilePath := "/opt/scrutiny/config/scrutiny.yaml"
	configFilePathAlternative := "/opt/scrutiny/config/scrutiny.yml"
	if !utils.FileExists(configFilePath) && utils.FileExists(configFilePathAlternative) {
		configFilePath = configFilePathAlternative
	}

	err = config.ReadConfig(configFilePath, bootstrapLogger)
	// Exit if there are errors with the default config, unless it doesn't exist
	if _, missing := err.(errors.ConfigFileMissingError); err != nil && !missing {
		bootstrapLogger.Error(color.HiRedString("CONFIG ERROR: %v", err))
		os.Exit(1)
	}

	flags := map[string]cli.Flag{
		"config": &cli.StringFlag{
			Name:  "config",
			Aliases: []string{"C"},
			Usage: "Specify the path to the config file",
			Action: func(c *cli.Context, filePath string) error {
				if filePath != "" {
					if err := config.ReadConfig(filePath, bootstrapLogger); err != nil {
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
		UsageText: "scrutiny [global options] [COMMAND [command options]]",
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

			banner := fmt.Sprintf(utils.StripIndent(
				`
			 ___   ___  ____  __  __  ____  ____  _  _  _  _
			/ __) / __)(  _ \(  )(  )(_  _)(_  _)( \( )( \/ )
			\__ \( (__  )   / )(__)(   )(   _)(_  )  (  \  /
			(___/ \___)(_)\_)(______) (__) (____)(_)\_) (__)
			%s

			`), subtitle)
			color.New(color.FgGreen).Fprintf(c.App.Writer, "%s", banner)

			return nil
		},
		Flags: []cli.Flag{flags["config"]},
		Commands: []*cli.Command{
			{
				Name: "device",
				Usage: "Tools to manage disk metadata",
				UsageText: "scrutiny [-C config] device [-h] [COMMAND [command options]]",
				Subcommands: []*cli.Command{{
					Name: "list",
					Usage: "Get information about all devices in Scrutiny",
					Action: func(c *cli.Context) error {
						db, err := database.NewScrutinyRepository(config, bootstrapLogger)
						if err != nil {
							panic(err)
						}
						return deviceListAction(c, db)
					},

				}, {
					Name: "patch",
					Usage: "Scan for and/or patch metadata discrepencies",
					UsageText: "scrutiny [-C config.yaml] device patch [options]",
					Description:
						"An interactive tool to help identify and resolve potential issues with device information.\n" +
						"When run without options, it will autodetect any devices with legacy serial-based fallback WWNs.\n" +
						"Select a device at the prompt and you'll be shown a comparison of the matching duplicate device.\n" +
						"Once you've confirmed the results are indeed duplicates, you can proceed to automerge the data.\n\n" +
						"Automatic merge will find all history entries for the old device in Influxdb and replace\n" +
						"their UUIDs with the correctly regenerated UUID based on v0.9.0+ empty WWN fallbacks.\n" +
						"Once historical data is merged, the creation date of the new SQLite entry will be set\n" +
						"to that of the original (ghost) device before removing the ghost from the database.\n\n" +
						"Note: If no clone is found, this tool will instead update the UUID and WWN of the ghost,\n" +
						"      so next time the respective collector is run it will already have the correct UUID.\n" +
						"      This assumes collectors are updated to at least v0.9. If you still have collectors\n" +
						"      on an older version, it's best to stop and update them BEFORE fixing legacy WWNs.\n" +
						"      If you patch ghosts with a legacy collector active, it could re-create the ghost.",
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
				UsageText: "scrutiny [-C config.yaml] start [options]",
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
		bootstrapLogger.Fatal(color.HiRedString("ERROR: %v", err))
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
