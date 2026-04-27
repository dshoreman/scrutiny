package main

import (
	"fmt"
	"strings"

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
