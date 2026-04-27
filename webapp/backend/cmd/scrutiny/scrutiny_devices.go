package main

import (
	"fmt"
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
	devices, err := loadDevices(c, db)
	if err != nil {
		return err
	}

	var diskRows, uuidRows [][]string
	var ghosts []models.Device

	lengths := []int{36 + len("Actual UUID: "), 0, 0}
	for _, device := range devices {
		// If there's no serial or WWN doesn't match it, it's not the ghost we're looking for
		if len(device.SerialNumber) > 0 && device.WWN == strings.ToLower(device.SerialNumber) {
			// Recalculate ScrutinyUUID to adjust for removed Serial WWN fallback in v0.9.{0,1}
			newUUID := detect.GenerateScrutinyUUID(device.ModelName, device.SerialNumber, "").String()
			row := []string{
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
		fmt.Printf("  > %s \t%s\t%s\n    %s \t%s\n",
			rpad(row[0], lengths[0]), rpad(row[1], lengths[1]), row[2],
			rpad(uuidRows[i][0], lengths[0]), uuidRows[i][1])
	}
	return nil
}

func loadDevices(c *cli.Context, db database.DeviceRepo) ([]models.Device, error) {
	fmt.Printf("\n Loading devices...\n")
	devices, err := db.GetDevices(c.Context)
	if err != nil {
		return nil, fmt.Errorf("Could not load devices: %v", err)
	}
	return devices, nil
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
