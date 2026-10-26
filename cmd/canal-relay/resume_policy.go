package main

import (
	"errors"
	"github.com/go-mysql-org/go-mysql/mysql"
)

func validateResumePolicy(saved mysql.Position, recovered, allowBootstrap, allowGap bool) error {
	if saved.Name == "" && !allowBootstrap {
		return errors.New("missing Canal checkpoint requires reviewed bootstrap")
	}
	if recovered && !allowGap {
		return errors.New("purged Canal checkpoint requires reviewed Outbox reconciliation")
	}
	return nil
}
