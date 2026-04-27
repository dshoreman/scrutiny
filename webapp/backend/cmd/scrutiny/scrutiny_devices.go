package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/analogj/scrutiny/collector/pkg/detect"
	"github.com/analogj/scrutiny/webapp/backend/pkg/database"
	"github.com/analogj/scrutiny/webapp/backend/pkg/models"

	"github.com/urfave/cli/v2"
)

func deviceListAction(c *cli.Context, db database.DeviceRepo) error {
	devices, err := loadDevices(c, db)
	if err != nil {
		return err
	}

	var rows [][]string
	lengths := []int{0, 0}
	for _, device := range devices {
		row := []string{fmt.Sprintf("  > %s/%s", device.HostId, device.DeviceName),
			fmt.Sprintf("- %s with serial '%s'", device.ModelName, device.SerialNumber)}
		rows = append(rows, row)
		setLengths(row, lengths)
	}

	fmt.Printf("  Found %d results\n\n", len(devices))
	for _, row := range rows {
		fmt.Println(rpad(row[0], lengths[0]), row[1])
	}
	return nil
}

func devicePatchAction(c *cli.Context, db database.DeviceRepo) error {
	var disk models.Device
	screen := "detect"
	for {
		devices, err := loadDevices(c, db)
		if err != nil {
			return err
		}

		reload:
		for {
			switch screen {
			case "detect":
				ghosts := devicePatchList(c, devices)
				fmt.Printf("\n  [0-%d] Review selected ghost\n\n", len(ghosts)-1)
				fmt.Println("  [r] Refresh devices")
				fmt.Println("  [q] Quit")
				for {
					switch reply := lineFromStdIn(); reply {
						case "r": break reload
						case "q": return cli.Exit("Goodbye!", 0)
						default:
							if i, err := strconv.Atoi(reply); err == nil && i >= 0 && i < len(ghosts) {
								disk = ghosts[i]
								fmt.Printf("Selected ghost %d, %s/%s\n", i, disk.HostId, disk.DeviceName)
								return nil
							}
							fmt.Println("Invalid selection")
					}
				}
			}
		}
	}
}

func devicePatchList(c *cli.Context, devices []models.Device) []models.Device {
	var diskRows, uuidRows [][]string
	var ghosts []models.Device

	lengths := []int{0, 36 + len("Actual UUID: "), 0, 0}
	for _, device := range devices {
		// If there's no serial or WWN doesn't match it, it's not the ghost we're looking for
		if len(device.SerialNumber) > 0 && device.WWN == strings.ToLower(device.SerialNumber) {
			// Recalculate ScrutinyUUID to adjust for removed Serial WWN fallback in v0.9.{0,1}
			newUUID := detect.GenerateScrutinyUUID(device.ModelName, device.SerialNumber, "").String()
			row := []string{fmt.Sprintf("[%d]", len(ghosts)),
				fmt.Sprintf("%s/%s - %s", device.HostId, device.DeviceName, device.ModelName),
				fmt.Sprintf("Serial '%s',", device.SerialNumber),
				fmt.Sprintf("WWN: '%s'", device.WWN)}

			urow := []string{"Actual UUID: " + device.ScrutinyUUID.String(), "Recalculated UUID: " + newUUID}
			ghosts, diskRows, uuidRows = append(ghosts, device), append(diskRows, row), append(uuidRows, urow)
			setLengths(row, lengths)
		}
	}

	fmt.Printf("  Scan of %d devices found %d ghosts:\n\n", len(devices), len(ghosts))
	for i, row := range diskRows {
		fmt.Printf("  %s %s \t%s\t%s\n  %s %s \t%s\n",
			lpad(row[0], lengths[0]), rpad(row[1], lengths[1]), rpad(row[2], lengths[2]), row[3],
			lpad("", lengths[0]), rpad(uuidRows[i][0], lengths[1]), uuidRows[i][1])
	}
	return ghosts
}

func lineFromStdIn() string {
	var selection string
	fmt.Print("\nEnter selection: ")
	fmt.Scanln(&selection)

	return selection
}

func loadDevices(c *cli.Context, db database.DeviceRepo) ([]models.Device, error) {
	fmt.Printf("\n Loading devices...\n")
	devices, err := db.GetSortedDevices(c.Context)
	if err != nil {
		return nil, fmt.Errorf("Could not load devices: %v", err)
	}
	return devices, nil
}

func lpad(str string, length int) string {
	return strings.Repeat(" ", length - len(str)) + str
}
func rpad(str string, length int) string {
	return str + strings.Repeat(" ", length - len(str))
}

func setLengths(row []string, lengths []int) {
	for i, v := range row {
		if length := len(v); length > lengths[i] {
			lengths[i] = length
		}
	}
}
