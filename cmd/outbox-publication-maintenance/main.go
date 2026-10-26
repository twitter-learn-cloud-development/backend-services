package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"twitter-clone/internal/infrastructure/canal"
	"twitter-clone/internal/infrastructure/mq"
	"twitter-clone/internal/module/tweet/projection"
	repository "twitter-clone/internal/module/tweet/repository"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("outbox-publication-maintenance", flag.ContinueOnError)
	mode := flags.String("mode", "inspect", "inspect, replay or retention-plan; no delete mode")
	allow := flags.Bool("allow-live", false, "confirmed dependency access")
	write := flags.Bool("allow-write", false, "explicitly reviewed bounded event replay")
	idValue := flags.String("event-ids", "", "1-100 explicit OutboxEvent IDs")
	dsnEnv := flags.String("dsn-env", "PUBLICATION_MYSQL_DSN", "credential reference")
	approved := flags.String("approve-plan", "", "reviewed plan digest")
	operation := flags.String("operation-id", "", "stable operation identity")
	operator := flags.String("operator", "", "operator attestation")
	reason := flags.String("reason", "", "replay reason")
	if flags.Parse(args) != nil {
		return errors.New("invalid maintenance options")
	}
	ids := []uint64{}
	for _, part := range strings.Split(*idValue, ",") {
		id, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return errors.New("invalid event scope")
		}
		ids = append(ids, id)
	}
	ids, err := projection.NormalizeIDs(ids)
	if err != nil {
		return err
	}
	if *mode != "inspect" && *mode != "replay" && *mode != "retention-plan" {
		return errors.New("invalid maintenance mode")
	}
	auth := projection.RepairAuthorization{ApprovedPlan: *approved, Operation: *operation, Operator: *operator, Reason: *reason}
	if *allow && *mode == "replay" && (!*write || !auth.Valid()) {
		return errors.New("replay requires explicit authorization and reviewed plan")
	}
	if !*allow {
		return json.NewEncoder(out).Encode(map[string]interface{}{"version": "publication-maintenance-v1", "dry_run": true, "mode": *mode, "scope_count": len(ids), "deletion_supported": false})
	}
	if *dsnEnv == "" {
		return errors.New("explicit database target required")
	}
	config, err := mysqlDriver.ParseDSN(os.Getenv(*dsnEnv))
	if err != nil || config.User == "" || config.DBName == "" || config.MultiStatements {
		return errors.New("invalid publication database target")
	}
	config.Timeout = 5 * time.Second
	config.ReadTimeout = 5 * time.Second
	config.WriteTimeout = 5 * time.Second
	connector, err := mysqlDriver.NewConnector(config)
	if err != nil {
		return errors.New("publication connector unavailable")
	}
	pool := sql.OpenDB(connector)
	defer pool.Close()
	pool.SetMaxOpenConns(1)
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: pool, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return errors.New("publication repository unavailable")
	}
	mqConfig := mq.DefaultRabbitMQConfig()
	target := projection.Hash([]byte(fmt.Sprintf("%s|%s|%s|%s|%d|%s", config.Net, config.Addr, config.DBName, mqConfig.Host, mqConfig.Port, mqConfig.Vhost)))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	maintenance := projection.PublicationMaintenance{Repository: repository.NewOutboxPublicationRepository(db), Target: target, Now: time.Now}
	var plan projection.PublicationPlan
	count := 0
	if *mode == "replay" {
		// Check the plan before opening a Broker connection.
		plan, err = maintenance.Inspect(ctx, ids)
		if err != nil {
			return err
		}
		if plan.Digest != *approved {
			return errors.New("publication plan changed")
		}
		broker, err := mq.NewRabbitMQWithContext(ctx, mqConfig)
		if err != nil {
			return errors.New("confirmed publication broker unavailable")
		}
		defer broker.Close()
		publisher, err := canal.NewConfirmedRelayPublisher(broker)
		if err != nil {
			return errors.New("publication confirms unavailable")
		}
		maintenance.Publisher = publisher
		plan, count, err = maintenance.Replay(ctx, ids, auth)
	} else {
		plan, err = maintenance.Inspect(ctx, ids)
	}
	if err != nil {
		_ = json.NewEncoder(out).Encode(map[string]interface{}{"complete": false, "confirmed_events": count, "plan_digest": plan.Digest, "deletion_supported": false})
		return err
	}
	return json.NewEncoder(out).Encode(struct {
		Plan              projection.PublicationPlan `json:"plan"`
		Confirmed         int                        `json:"confirmed_events"`
		ReadOnly          bool                       `json:"read_only"`
		DeletionSupported bool                       `json:"deletion_supported"`
	}{plan, count, *mode != "replay", false})
}
