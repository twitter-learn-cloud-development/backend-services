package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/go-redis/redis/v8"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
	"twitter-clone/internal/infrastructure/cache"
	"twitter-clone/internal/module/agent/admission"
)

type controller interface {
	PolicyDigest() string
	Ready(context.Context) error
	Initialize(context.Context) error
}
type factory func(context.Context, admission.Config) (controller, func(), error)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.LookupEnv, connect); err != nil {
		fmt.Fprintln(os.Stderr, "admission administration stopped:", err)
		os.Exit(1)
	}
}
func run(args []string, out io.Writer, lookup func(string) (string, bool), open factory) error {
	flags := flag.NewFlagSet("agent-admission-admin", flag.ContinueOnError)
	flags.SetOutput(out)
	live := flags.Bool("allow-live", false, "read explicit Redis policy state")
	initialize := flags.Bool("initialize", false, "initialize matching policy without clearing leases")
	operator := flags.String("operator", "", "required for initialization")
	reason := flags.String("reason", "", "required for initialization")
	approved := flags.String("approved-policy", "", "reviewed policy digest required for initialization")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected argument")
	}
	cfg, _, err := admission.FromEnv(lookup)
	if err != nil {
		return err
	}
	// Offline digest uses policy serialization, without constructing Redis.
	digest := admission.ConfigDigest(cfg)
	if *initialize && (!*live || *approved != digest || strings.TrimSpace(*operator) == "" || strings.TrimSpace(*reason) == "" || len(*operator) > 128 || len(*reason) > 512) {
		return errors.New("initialization requires explicit live access, matching policy and bounded operator/reason")
	}
	if !*live {
		return json.NewEncoder(out).Encode(struct {
			Mode   string           `json:"mode"`
			Policy string           `json:"policy_sha256"`
			Config admission.Config `json:"config"`
		}{"offline", digest, cfg})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctrl, close, err := open(ctx, cfg)
	if err != nil {
		return errors.New("admission Redis unavailable")
	}
	defer close()
	mode := "ready"
	if *initialize {
		mode = "initialized"
		err = ctrl.Initialize(ctx)
	} else {
		err = ctrl.Ready(ctx)
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(struct{ Mode, Policy, OperatorSHA256, ReasonSHA256 string }{mode, digest, admission.ValueDigest(*operator), admission.ValueDigest(*reason)})
}
func connect(ctx context.Context, cfg admission.Config) (controller, func(), error) {
	redisConfig := cache.DefaultRedisConfig()
	client := redis.NewClient(&redis.Options{
		Addr:     net.JoinHostPort(redisConfig.Host, strconv.Itoa(redisConfig.Port)),
		Password: redisConfig.Password, DB: redisConfig.DB,
		PoolSize: 1, MaxRetries: -1, DialTimeout: 2 * time.Second,
		ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second,
	})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, nil, err
	}
	gate, err := admission.New(client, cfg, nil)
	if err != nil {
		_ = client.Close()
		return nil, nil, err
	}
	return gate, func() { _ = client.Close() }, nil
}
