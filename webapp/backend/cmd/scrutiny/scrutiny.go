package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	_ "go.uber.org/automaxprocs"

	log "github.com/sirupsen/logrus"
	utils "github.com/analogj/go-util/utils"
	"github.com/analogj/scrutiny/webapp/backend/pkg/config"
	"github.com/analogj/scrutiny/webapp/backend/pkg/database"
	"github.com/analogj/scrutiny/webapp/backend/pkg/version"
	"github.com/analogj/scrutiny/webapp/backend/pkg/web"
	"github.com/fatih/color"
	"github.com/urfave/cli/v2"
)

var goos string
var goarch string

func init() {
	// Initially display time in logs; switch to runtime on server start
	log.SetLevel(log.WarnLevel)
	log.SetFormatter(&log.TextFormatter{
		DisableLevelTruncation: true,
		FullTimestamp: true,
		TimestampFormat: time.TimeOnly,
		PadLevelText: true,
	})
}

func main() {
	var vLevel int
	config, err := config.Create()
	if err != nil {
		log.Fatalf("FATAL: %+v\n", err)
	}

	flags := map[string]cli.Flag{
		"config": &cli.StringFlag{
			Name:  "config",
			Aliases: []string{"C"},
			Usage: "Specify the path to the config file",
			Action: func(c *cli.Context, filePath string) error {
				if filePath != "" {
					return config.ReadConfig(filePath)
				}
				return nil
			},
		},
		"verbose": &cli.BoolFlag{
			Name: "verbose",
			Count: &vLevel,
			Aliases: []string{"V"},
			EnvVars: []string{"SCRUTINY_DEBUG", "DEBUG"},
			Usage: "Enable verbose output; passing twice enables debug messages.",
			Action: func(c *cli.Context, enabled bool) error {
				// Never called if vLevel == 0 (-v wasn't passed), but enabled
				// can still be false if someone passed e.g. '-VV -V=false'.
				log.Warnf("Verbose flag passed %d times! Enabled=%+v", vLevel, enabled)
				if enabled && vLevel >= 2 {
					log.SetLevel(log.DebugLevel)
				} else if enabled {
					log.SetLevel(log.InfoLevel)
				} else {
					// Scrutiny doesn't really use warnings yet. This simple ensures
					// any future warnings aren't accidentally hidden, while still
					// supressing general info messages in non-server commands.
					log.SetLevel(log.WarnLevel)
				}
				return nil
			},
		},
	}

	app := &cli.App{
		Name:     "scrutiny",
		Usage:    "WebUI for smartd S.M.A.R.T monitoring",
		UsageText: "scrutiny [global options] [COMMAND [command options]]",
		UseShortOptionHandling: true,
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

			banner := fmt.Sprintf(utils.StripIndent(`
				 ___   ___  ____  __  __  ____  ____  _  _  _  _
				/ __) / __)(  _ \(  )(  )(_  _)(_  _)( \( )( \/ )
				\__ \( (__  )   / )(__)(   )(   _)(_  )  (  \  /
				(___/ \___)(_)\_)(______) (__) (____)(_)\_) (__)
				%s
			`), subtitle)
			color.New(color.FgGreen).Fprintf(c.App.Writer, "%s\n", banner)

			configFilePath := "/opt/scrutiny/config/scrutiny.yaml"
			configFilePathAlternative := "/opt/scrutiny/config/scrutiny.yml"
			if !utils.FileExists(configFilePath) {
				configFilePath = configFilePathAlternative
			}
			// Only attempt to load default config if it (or the Alternative) exists
			if configFilePath != configFilePathAlternative || utils.FileExists(configFilePath) {
				if err = config.ReadConfig(configFilePath); err != nil {
					log.Fatal(color.HiRedString("CONFIG ERROR: %v", err))
				}
			} else if !c.IsSet("config") {
				// Warn if no global config was found, but avoid double-logging if a local file is given
				log.Warn("No configuration file found. Using defaults.")
				log.Warn("To hide this warning, pass --config or create /opt/scrutiny/config/scrutiny.yaml")
			}

			return nil
		},
		Flags: []cli.Flag{flags["config"], flags["verbose"]},
		Commands: []*cli.Command{
			{
				Name: "device",
				Usage: "Tools to manage disk metadata",
				UsageText: "scrutiny [-C config] device [-h] [COMMAND [command options]]",
				Subcommands: []*cli.Command{{
					Name: "list",
					Usage: "Get information about all devices in Scrutiny",
					Action: func(c *cli.Context) error {
						db, err := database.NewScrutinyRepository(config, plainLogger())
						if err != nil {
							log.Fatal(err)
						}
						return deviceListAction(c, db)
					},

				}, {
					Name: "patch",
					Flags: []cli.Flag{flags["verbose"]},
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
						db, err := database.NewScrutinyRepository(config, plainLogger())
						if err != nil {
							log.Fatal(err)
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
						Aliases: []string{"V"},
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
					log.Println("Starting Scrutiny server")
					webLogger, logFile, err := CreateWebLogger(config)
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

func CreateWebLogger(appConfig config.Interface) (*log.Entry, *os.File, error) {
	log.SetFormatter(&log.TextFormatter{})
	if level, err := log.ParseLevel(appConfig.GetString("log.level")); err == nil {
		log.SetLevel(level)
	} else {
		log.SetLevel(log.InfoLevel)
	}

	logger := log.WithFields(log.Fields{"type": "web"})
	logFilePath := appConfig.GetString("log.file")
	if len(logFilePath) == 0 {
		return logger, nil, nil
	}

	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		logger.Errorf("Failed to open log file %s for output: %s", logFilePath, err)
		return nil, logFile, err
	}

	logger.Logger.SetOutput(io.MultiWriter(os.Stderr, logFile))
	return logger, logFile, nil
}

func plainLogger() log.FieldLogger {
	return log.WithFields(log.Fields{})
}
