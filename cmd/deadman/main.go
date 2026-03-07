// deadman — SMS deadman switch with multi-owner, subscriber consent, and admin polls.
//
// Usage:
//
//	deadman serve                                  start HTTP server + ticker
//	deadman owner add <phone> [name]               register an owner
//	deadman owner list                             list all owners + status
//	deadman owner remove <phone>                   remove an owner
//	deadman subscriber add <phone> [name]          register a subscriber record
//	deadman subscriber list                        list all subscribers
//	deadman subscriber remove <phone>              remove a subscriber
//	deadman subscribe <owner> <subscriber>         link owner→subscriber (sends consent SMS)
//	deadman subscriptions [--owner <phone>]        list subscriptions
//	deadman admin add <phone> [name]               add admin (receives W/H poll alerts)
//	deadman admin list                             list admins
//	deadman admin remove <phone>                   remove admin
//	deadman status [<owner-phone>]                 print timer status
//
// Environment variables:
//
//	TWILIO_ACCOUNT_SID   required
//	TWILIO_AUTH_TOKEN    required
//	TWILIO_FROM          required  E.164 Twilio number
//	DEADMAN_PUBLIC_URL   optional  public base URL (e.g. https://nexus-deadman-dev-xxx.run.app)
//	                               when set, configures Twilio SMS webhook on startup
//	PORT                 optional  default 8095
//	DATABASE_URL         optional  default host=/tmp/ctl-pg dbname=deadman user=jredh
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jredh-dev/nexus/internal/deadman"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	args := os.Args[1:]
	if len(args) == 0 || isHelp(args[0]) {
		printUsage()
		os.Exit(0)
	}

	cfg := loadConfig()
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, cfg.databaseURL)
	if err != nil {
		fatalf("db connect: %v", err)
	}
	defer pool.Close()

	if err := deadman.Migrate(ctx, pool); err != nil {
		fatalf("migrate: %v", err)
	}

	twilio := deadman.TwilioConfig{
		AccountSID: cfg.twilioSID,
		AuthToken:  cfg.twilioToken,
		From:       cfg.twilioFrom,
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "serve":
		cmdServe(ctx, pool, twilio, cfg.port)

	case "owner":
		cmdOwner(ctx, pool, twilio, rest)

	case "subscriber":
		cmdSubscriber(ctx, pool, rest)

	case "subscribe":
		cmdSubscribe(ctx, pool, twilio, rest)

	case "subscriptions":
		cmdSubscriptions(ctx, pool, rest)

	case "admin":
		cmdAdmin(ctx, pool, rest)

	case "status":
		cmdStatus(ctx, pool, rest)

	default:
		fatalf("unknown command: %s\nRun deadman --help for usage.", cmd)
	}
}

// -----------------------------------------------------------------------
// serve
// -----------------------------------------------------------------------

func cmdServe(ctx context.Context, pool *pgxpool.Pool, twilio deadman.TwilioConfig, port string) {
	// Auto-configure Twilio webhook if DEADMAN_PUBLIC_URL is set.
	// Format: https://nexus-deadman-dev-xxx.run.app  (no trailing slash)
	if publicURL := os.Getenv("DEADMAN_PUBLIC_URL"); publicURL != "" {
		smsURL := strings.TrimRight(publicURL, "/") + "/sms"
		slog.Info("configuring Twilio webhook", "url", smsURL)
		if err := deadman.ConfigureWebhook(twilio, smsURL); err != nil {
			// Non-fatal: log and continue.  SMS won't arrive until fixed, but
			// the server is still useful for outbound sends and CLI commands.
			slog.Error("failed to configure Twilio webhook", "err", err)
		}
	} else {
		slog.Warn("DEADMAN_PUBLIC_URL not set — Twilio webhook not auto-configured; set it to your Cloud Run URL")
	}

	go deadman.RunTicker(ctx, pool, twilio)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /sms", deadman.MakeSMSHandler(pool, twilio))
	mux.HandleFunc("GET /status", deadman.MakeStatusHandler(pool))
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "ok")
	})

	addr := ":" + port
	slog.Info("deadman serving", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		fatalf("serve: %v", err)
	}
}

// -----------------------------------------------------------------------
// owner subcommands
// -----------------------------------------------------------------------

func cmdOwner(ctx context.Context, pool *pgxpool.Pool, twilio deadman.TwilioConfig, args []string) {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Println("Usage: deadman owner add <phone> [name]\n       deadman owner list\n       deadman owner remove <phone>")
		return
	}
	switch args[0] {
	case "add":
		if len(args) < 2 {
			fatalf("usage: deadman owner add <phone> [name]")
		}
		phone := args[1]
		name := ""
		if len(args) >= 3 {
			name = strings.Join(args[2:], " ")
		}
		o, err := deadman.AddOwner(ctx, pool, phone, name, 72*time.Hour, 72*time.Hour, 96*time.Hour)
		if err != nil {
			fatalf("add owner: %v", err)
		}
		fmt.Printf("Owner added: %s (%s) id=%d\n", o.Phone, o.Name, o.ID)

	case "list":
		owners, err := deadman.ListOwners(ctx, pool)
		if err != nil {
			fatalf("list owners: %v", err)
		}
		if len(owners) == 0 {
			fmt.Println("No owners.")
			return
		}
		fmt.Printf("%-5s %-16s %-20s %s\n", "ID", "PHONE", "NAME", "STATUS")
		fmt.Println(strings.Repeat("-", 80))
		for _, o := range owners {
			fmt.Printf("%-5d %-16s %-20s %s\n", o.ID, o.Phone, o.Name, deadman.OwnerStatus(o))
		}

	case "remove":
		if len(args) < 2 {
			fatalf("usage: deadman owner remove <phone>")
		}
		if err := deadman.RemoveOwner(ctx, pool, args[1]); err != nil {
			fatalf("remove owner: %v", err)
		}
		fmt.Printf("Owner removed: %s\n", args[1])

	default:
		fatalf("unknown owner subcommand: %s", args[0])
	}
}

// -----------------------------------------------------------------------
// subscriber subcommands
// -----------------------------------------------------------------------

func cmdSubscriber(ctx context.Context, pool *pgxpool.Pool, args []string) {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Println("Usage: deadman subscriber add <phone> [name]\n       deadman subscriber list\n       deadman subscriber remove <phone>")
		return
	}
	switch args[0] {
	case "add":
		if len(args) < 2 {
			fatalf("usage: deadman subscriber add <phone> [name]")
		}
		phone := args[1]
		name := ""
		if len(args) >= 3 {
			name = strings.Join(args[2:], " ")
		}
		s, err := deadman.AddSubscriber(ctx, pool, phone, name)
		if err != nil {
			fatalf("add subscriber: %v", err)
		}
		fmt.Printf("Subscriber added: %s (%s) id=%d\n", s.Phone, s.Name, s.ID)

	case "list":
		subs, err := deadman.ListSubscribers(ctx, pool)
		if err != nil {
			fatalf("list subscribers: %v", err)
		}
		if len(subs) == 0 {
			fmt.Println("No subscribers.")
			return
		}
		fmt.Printf("%-5s %-16s %s\n", "ID", "PHONE", "NAME")
		fmt.Println(strings.Repeat("-", 50))
		for _, s := range subs {
			fmt.Printf("%-5d %-16s %s\n", s.ID, s.Phone, s.Name)
		}

	case "remove":
		if len(args) < 2 {
			fatalf("usage: deadman subscriber remove <phone>")
		}
		if err := deadman.RemoveSubscriber(ctx, pool, args[1]); err != nil {
			fatalf("remove subscriber: %v", err)
		}
		fmt.Printf("Subscriber removed: %s\n", args[1])

	default:
		fatalf("unknown subscriber subcommand: %s", args[0])
	}
}

// -----------------------------------------------------------------------
// subscribe
// -----------------------------------------------------------------------

func cmdSubscribe(ctx context.Context, pool *pgxpool.Pool, twilio deadman.TwilioConfig, args []string) {
	if len(args) < 2 || isHelp(args[0]) {
		fatalf("usage: deadman subscribe <owner-phone> <subscriber-phone>")
	}
	ownerPhone := args[0]
	subPhone := args[1]

	sub, err := deadman.Subscribe(ctx, pool, ownerPhone, subPhone)
	if err != nil {
		fatalf("subscribe: %v", err)
	}

	// Send consent SMS.
	msg := deadman.ConsentSMS(ownerPhone)
	if err := deadman.SendSMS(twilio, subPhone, msg); err != nil {
		slog.Error("consent SMS failed", "to", subPhone, "err", err)
		fmt.Printf("Subscription created (id=%d, status=pending) but consent SMS failed: %v\n", sub.ID, err)
		return
	}
	fmt.Printf("Subscription created (id=%d, status=pending). Consent SMS sent to %s.\n", sub.ID, subPhone)
}

// -----------------------------------------------------------------------
// subscriptions
// -----------------------------------------------------------------------

func cmdSubscriptions(ctx context.Context, pool *pgxpool.Pool, args []string) {
	ownerPhone := ""
	for i, a := range args {
		if a == "--owner" && i+1 < len(args) {
			ownerPhone = args[i+1]
		}
	}

	var subs []deadman.Subscription
	var err error
	if ownerPhone != "" {
		subs, err = deadman.ListSubscriptionsByOwner(ctx, pool, ownerPhone)
	} else {
		// List all: iterate owners.
		owners, oerr := deadman.ListOwners(ctx, pool)
		if oerr != nil {
			fatalf("list owners: %v", oerr)
		}
		for _, o := range owners {
			s, serr := deadman.ListSubscriptionsByOwner(ctx, pool, o.Phone)
			if serr != nil {
				fatalf("list subscriptions: %v", serr)
			}
			subs = append(subs, s...)
		}
	}
	if err != nil {
		fatalf("list subscriptions: %v", err)
	}
	if len(subs) == 0 {
		fmt.Println("No subscriptions.")
		return
	}
	fmt.Printf("%-5s %-16s %-20s %-16s %-20s %s\n", "ID", "OWNER", "OWNER NAME", "SUBSCRIBER", "SUBSCRIBER NAME", "STATUS")
	fmt.Println(strings.Repeat("-", 100))
	for _, s := range subs {
		fmt.Printf("%-5d %-16s %-20s %-16s %-20s %s\n",
			s.ID, s.OwnerPhone, s.OwnerName, s.SubscriberPhone, s.SubscriberName, s.Status)
	}
}

// -----------------------------------------------------------------------
// admin subcommands
// -----------------------------------------------------------------------

func cmdAdmin(ctx context.Context, pool *pgxpool.Pool, args []string) {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Println("Usage: deadman admin add <phone> [name]\n       deadman admin list\n       deadman admin remove <phone>")
		return
	}
	switch args[0] {
	case "add":
		if len(args) < 2 {
			fatalf("usage: deadman admin add <phone> [name]")
		}
		phone := args[1]
		name := ""
		if len(args) >= 3 {
			name = strings.Join(args[2:], " ")
		}
		a, err := deadman.AddAdmin(ctx, pool, phone, name)
		if err != nil {
			fatalf("add admin: %v", err)
		}
		fmt.Printf("Admin added: %s (%s) id=%d\n", a.Phone, a.Name, a.ID)

	case "list":
		admins, err := deadman.ListAdmins(ctx, pool)
		if err != nil {
			fatalf("list admins: %v", err)
		}
		if len(admins) == 0 {
			fmt.Println("No admins.")
			return
		}
		fmt.Printf("%-5s %-16s %s\n", "ID", "PHONE", "NAME")
		fmt.Println(strings.Repeat("-", 50))
		for _, a := range admins {
			fmt.Printf("%-5d %-16s %s\n", a.ID, a.Phone, a.Name)
		}

	case "remove":
		if len(args) < 2 {
			fatalf("usage: deadman admin remove <phone>")
		}
		if err := deadman.RemoveAdmin(ctx, pool, args[1]); err != nil {
			fatalf("remove admin: %v", err)
		}
		fmt.Printf("Admin removed: %s\n", args[1])

	default:
		fatalf("unknown admin subcommand: %s", args[0])
	}
}

// -----------------------------------------------------------------------
// status
// -----------------------------------------------------------------------

func cmdStatus(ctx context.Context, pool *pgxpool.Pool, args []string) {
	var owners []deadman.Owner
	var err error
	if len(args) > 0 && !isHelp(args[0]) {
		o, oerr := deadman.GetOwnerByPhone(ctx, pool, args[0])
		if oerr != nil {
			fatalf("get owner: %v", oerr)
		}
		owners = []deadman.Owner{o}
	} else {
		owners, err = deadman.ListOwners(ctx, pool)
		if err != nil {
			fatalf("list owners: %v", err)
		}
	}
	if len(owners) == 0 {
		fmt.Println("No owners registered.")
		return
	}
	for _, o := range owners {
		fmt.Printf("%s (%s): %s\n", o.Phone, o.Name, deadman.OwnerStatus(o))
	}
}

// -----------------------------------------------------------------------
// Config
// -----------------------------------------------------------------------

type appConfig struct {
	twilioSID   string
	twilioToken string
	twilioFrom  string
	port        string
	databaseURL string
}

func loadConfig() appConfig {
	c := appConfig{
		twilioSID:   os.Getenv("TWILIO_ACCOUNT_SID"),
		twilioToken: os.Getenv("TWILIO_AUTH_TOKEN"),
		twilioFrom:  os.Getenv("TWILIO_FROM"),
		port:        os.Getenv("PORT"),
		databaseURL: os.Getenv("DATABASE_URL"),
	}
	if c.port == "" {
		c.port = "8095"
	}
	if c.databaseURL == "" {
		c.databaseURL = "host=/tmp/ctl-pg dbname=deadman user=jredh"
	}
	return c
}

// -----------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------

func isHelp(s string) bool {
	return s == "--help" || s == "-h" || s == "-?" || s == "help"
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}

func printUsage() {
	fmt.Print(`deadman — SMS deadman switch via Twilio

Commands:
  serve                                  Start HTTP server + ticker loop
  owner add <phone> [name]               Register an owner (72h default interval)
  owner list                             List owners with current timer status
  owner remove <phone>                   Remove an owner
  subscriber add <phone> [name]          Register a subscriber record
  subscriber list                        List all subscribers
  subscriber remove <phone>              Remove a subscriber
  subscribe <owner> <subscriber>         Link owner→subscriber, send consent SMS
  subscriptions [--owner <phone>]        List all subscriptions
  admin add <phone> [name]               Add an admin (receives W/H poll alerts)
  admin list                             List admins
  admin remove <phone>                   Remove an admin
  status [<owner-phone>]                 Print current timer status

Environment variables:
  TWILIO_ACCOUNT_SID   required
  TWILIO_AUTH_TOKEN    required
  TWILIO_FROM          required  E.164 Twilio number (e.g. +15706006135)
  DEADMAN_PUBLIC_URL   optional  public base URL (e.g. https://nexus-deadman-dev-xxx.run.app)
                                 when set, Twilio SMS webhook is auto-configured on startup
  PORT                 optional  HTTP listen port (default: 8095)
  DATABASE_URL         optional  PostgreSQL DSN
                                 (default: host=/tmp/ctl-pg dbname=deadman user=jredh)

HTTP endpoints (when serving):
  POST /sms     Twilio inbound SMS webhook
  GET  /status  JSON owner status
  GET  /health  200 OK

SMS protocol:
  Owner texts:      any message → check-in, timer reset
  Subscriber texts: R=status, W=ask why, H=ask how, U=unsubscribe all
  Consent texts:    Y=subscribe, N=decline, Q=block (never contact again)
`)
}
