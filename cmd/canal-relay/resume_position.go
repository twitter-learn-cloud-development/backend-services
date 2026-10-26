package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-mysql-org/go-mysql/mysql"
	"gorm.io/gorm"
)

type binaryLog struct {
	Name string `gorm:"column:Log_name"`
}

func availableBinaryLogs(db *gorm.DB) ([]string, error) {
	var logs []binaryLog
	if err := db.Raw("SHOW BINARY LOGS").Scan(&logs).Error; err != nil {
		return nil, fmt.Errorf("list MySQL binary logs: %w", err)
	}
	if len(logs) == 0 {
		return nil, errors.New("MySQL has no binary logs available")
	}
	names := make([]string, 0, len(logs))
	for _, log := range logs {
		if log.Name == "" {
			return nil, errors.New("MySQL returned an unnamed binary log")
		}
		names = append(names, log.Name)
	}
	return names, nil
}

func binlogSequence(name string) (string, uint64, error) {
	prefix := strings.TrimRight(name, "0123456789")
	if prefix == "" || len(prefix) == len(name) {
		return "", 0, fmt.Errorf("invalid binlog name %q", name)
	}
	number, err := strconv.ParseUint(name[len(prefix):], 10, 64)
	return prefix, number, err
}

// A purged checkpoint can only replay from the oldest retained log. Older
// events need separate Outbox reconciliation; never silently skip to the head.
func resolveResumePosition(saved mysql.Position, available []string) (mysql.Position, bool, error) {
	if len(available) == 0 {
		return mysql.Position{}, false, errors.New("MySQL has no binary logs available")
	}
	if saved.Name == "" {
		return saved, false, nil
	}
	for _, name := range available {
		if name == saved.Name {
			return saved, false, nil
		}
	}
	savedPrefix, savedNumber, err := binlogSequence(saved.Name)
	if err != nil {
		return mysql.Position{}, false, err
	}
	oldestPrefix, oldestNumber, err := binlogSequence(available[0])
	if err != nil {
		return mysql.Position{}, false, err
	}
	if savedPrefix != oldestPrefix || savedNumber >= oldestNumber {
		return mysql.Position{}, false, fmt.Errorf("saved binlog %q is not in current MySQL history", saved.Name)
	}
	return mysql.Position{Name: available[0], Pos: 4}, true, nil
}
