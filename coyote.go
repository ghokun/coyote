package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/cqroot/prompt"
	"github.com/cqroot/prompt/choose"
	"github.com/fatih/color"
	"github.com/rabbitmq/amqp091-go"
	"github.com/urfave/cli/v3"
	_ "modernc.org/sqlite"

	failed "github.com/ghokun/coyote/error"
	"github.com/ghokun/coyote/internal/api"
	"github.com/ghokun/coyote/internal/consume"
	"github.com/ghokun/coyote/internal/daemon"
	"github.com/ghokun/coyote/internal/ipc"
)

var Version = "development"

const description = `Coyote is a RabbitMQ message sink.

Creates an interceptor queue for the given exchange/routing-key pairs and
captures messages. The same binary acts as CLI and background daemon:
'consume' auto-starts the daemon when needed.

Exchange binding formats:
 --exchange myexchange=#                          # All messages in single exchange
 --exchange myexchange1=mykey1                    # Messages with routing key in a single exchange
 --exchange myexchange1=mykey1,myexchange1=mykey2 # Messages with routing keys in a single exchange
 --exchange myexchange1=#,myexchange2=#           # All messages in multiple exchanges
 --exchange myexchange1=mykey1,myexchange2=mykey2 # Messages with routing keys in multiple exchanges
 --exchange myexchange1=#,myexchange2=mykey2      # Messages with or without specific routing keys in multiple exchanges`

func consumeFlags() []cli.Flag {
	return consumeFlagsRequired(true)
}

// rootFlags mirrors consumeFlags but without Required: root-level Required
// flags would be enforced for every subcommand (list, daemon status, ...),
// so the compat path validates presence manually instead.
func rootFlags() []cli.Flag {
	return consumeFlagsRequired(false)
}

func consumeFlagsRequired(required bool) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:     "url",
			Required: required,
			Usage:    "RabbitMQ url, must start with amqps:// or amqp://.",
		},
		&cli.BoolFlag{
			Name:  "oauth",
			Usage: "Use OAuth 2.0 for authentication.",
		},
		&cli.StringFlag{
			Name:  "redirect-url",
			Usage: "OIDC callback url for OAuth 2.0",
		},
		&cli.BoolFlag{
			Name:  "insecure",
			Usage: "Skips certificate verification.",
		},
		&cli.StringMapFlag{
			Name:        "exchange",
			Required:    required,
			Usage:       "Exchange & routing key combinations to listen messages.",
			DefaultText: "myexchange=#",
		},
		&cli.StringFlag{
			Name:  "queue",
			Usage: "Interceptor queue name. If provided, interceptor queue will not be auto deleted.",
		},
		&cli.StringFlag{
			Name:  "store",
			Usage: "SQLite filename to store events.",
		},
		&cli.BoolFlag{
			Name:  "silent",
			Usage: "Disables terminal print.",
		},
	}
}

func main() {
	app := &cli.Command{
		Name:        "coyote",
		Usage:       "Coyote is a RabbitMQ message sink.",
		Description: description,
		Version:     Version,
		Flags:       rootFlags(), // compat: bare `coyote --url ...` still runs in foreground
		Action:      runCompat,
		Commands: []*cli.Command{
			{
				Name:  "consume",
				Usage: "Consume messages via the daemon (auto-starts it when needed).",
				Flags: append(consumeFlags(),
					&cli.BoolFlag{Name: "foreground", Usage: "Run in the foreground without the daemon."},
				),
				Action: runConsume,
			},
			{
				Name:    "list",
				Aliases: []string{"ps"},
				Usage:   "List daemon tasks.",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "json", Usage: "Print tasks as JSON."},
				},
				Action: runList,
			},
			lifecycleCommand("start", "Start a stopped or failed task."),
			lifecycleCommand("stop", "Stop a running or paused task (keeps the queue)."),
			lifecycleCommand("pause", "Pause a running task (keeps the queue)."),
			lifecycleCommand("resume", "Resume a paused task."),
			{
				Name:  "rm",
				Usage: "Stop and remove a task.",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "Skip confirmation prompts."},
					&cli.BoolFlag{Name: "delete-queue", Usage: "Also delete the interceptor queue from the broker."},
					&cli.BoolFlag{Name: "keep-queue", Usage: "Keep the interceptor queue on the broker (default)."},
					&cli.BoolFlag{Name: "purge", Usage: "Also delete the task's store file."},
				},
				Action: runRm,
			},
			{
				Name:  "logs",
				Usage: "Show a task's log.",
				Flags: []cli.Flag{
					&cli.IntFlag{Name: "tail", Value: 100, Usage: "Number of trailing lines to show."},
				},
				Action: runLogs,
			},
			{
				Name:    "fetch",
				Aliases: []string{"messages"},
				Usage:   "Fetch stored messages of a task through the daemon.",
				Flags: []cli.Flag{
					&cli.IntFlag{Name: "limit", Value: 100, Usage: "Max messages to fetch."},
					&cli.IntFlag{Name: "offset", Value: 0, Usage: "Result offset."},
					&cli.BoolFlag{Name: "json", Usage: "Print messages as JSON."},
					&cli.StringFlag{Name: "store", Usage: "Write fetched messages as JSON to file."},
					&cli.StringFlag{Name: "exchange", Usage: "Only messages whose exchange contains this substring."},
					&cli.StringFlag{Name: "routing-key", Usage: "Only messages whose routing key contains this substring."},
					&cli.StringFlag{Name: "correlation-id", Usage: "Only messages whose correlation id contains this substring."},
					&cli.StringFlag{Name: "reply-to", Usage: "Only messages whose reply-to contains this substring."},
					&cli.StringFlag{Name: "headers", Usage: "Only messages whose headers contain this substring."},
					&cli.StringFlag{Name: "body", Usage: "Only messages whose body contains this substring."},
				},
				Action: runFetch,
			},
			{
				Name:   "daemon",
				Usage:  "Manage the background daemon.",
				Hidden: true,
				Commands: []*cli.Command{
					{
						Name:  "start",
						Usage: "Start the daemon.",
						Flags: []cli.Flag{
							&cli.BoolFlag{Name: "foreground", Usage: "Run the daemon in the foreground."},
						},
						Action: runDaemonStart,
					},
					{
						Name:   "stop",
						Usage:  "Stop the daemon (drains all tasks).",
						Action: runDaemonStop,
					},
					{
						Name:   "status",
						Usage:  "Show daemon status.",
						Action: runDaemonStatus,
					},
				},
			},
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}

// runCompat keeps the legacy bare invocation working:
// `coyote --url ... --exchange ...` runs in the foreground.
func runCompat(ctx context.Context, cmd *cli.Command) error {
	if !cmd.IsSet("url") || !cmd.IsSet("exchange") {
		return failed.Because(`required flags "url, exchange" not set (see 'coyote consume --help')`, nil)
	}
	fmt.Fprintln(os.Stderr, color.YellowString("warning: bare flags are deprecated, use 'coyote consume ...' instead"))
	return runForeground(ctx, cmd)
}

func runConsume(ctx context.Context, cmd *cli.Command) error {
	if cmd.Bool("foreground") {
		return runForeground(ctx, cmd)
	}
	if err := ipc.EnsureRunning(); err != nil {
		return err
	}
	rawURL, insecure, err := ResolveURL(cmd)
	if err != nil {
		return err
	}
	task, err := ipc.NewClient().CreateTask(api.CreateTaskRequest{
		URL:         rawURL,
		OAuth:       cmd.Bool("oauth"),
		RedirectURL: cmd.String("redirect-url"),
		Insecure:    insecure,
		Exchanges:   cmd.StringMap("exchange"),
		Queue:       cmd.String("queue"),
		Store:       cmd.String("store"),
		Silent:      cmd.Bool("silent"),
	})
	if err != nil {
		return err
	}
	fmt.Printf("Created task %s (status %s, queue %s)\n", task.ID, task.Status, task.Queue)
	fmt.Printf("See logs with: coyote logs %s\n", shortID(task.ID))
	return nil
}

// runForeground consumes in the current process (legacy behavior).
func runForeground(ctx context.Context, cmd *cli.Command) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	rawURL, insecure, err := ResolveURL(cmd)
	if err != nil {
		return err
	}
	exchanges := cmd.StringMap("exchange")
	if len(exchanges) == 0 {
		return failed.Because("at least one exchange is required", nil)
	}
	queue := cmd.String("queue")
	queueExplicit := cmd.IsSet("queue")
	silent := cmd.Bool("silent")
	store := cmd.String("store")

	log.Printf("🚀 Starting coyote (%s)", color.YellowString(Version))

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			fmt.Print("\r")
			log.Printf("👋 Received an interrupt signal, shutting down...")
			cancel()
			if queueExplicit {
				if askDeleteQueue(queue) {
					if err := consume.DeleteQueue(rawURL, insecure, queue); err != nil {
						log.Printf("failed to delete queue: %v", err)
					} else {
						log.Printf("🗑️ Deleted persistent interceptor queue %s", color.YellowString(queue))
					}
				} else {
					log.Printf("💾 Persistent interceptor queue %s is not deleted", color.YellowString(queue))
				}
			} else {
				log.Printf("👻 Interceptor queue is ephemeral and will be deleted by itself")
			}
		case <-ctx.Done():
		}
		<-sigCh
		os.Exit(2)
	}()

	count := 0
	hooks := consume.Hooks{
		OnLog: func(format string, args ...any) {
			log.Printf(format, args...)
		},
		OnMessage: func(d amqp091.Delivery) {
			if !silent {
				log.Printf("📧 %s\n%s%s\n%s%s\n%s%s\n%s%s\n%s%s\n%s%s",
					color.YellowString("Received a message"),
					color.GreenString("# Exchange        : "), d.Exchange,
					color.GreenString("# Routing-key     : "), d.RoutingKey,
					color.GreenString("# Correlation-id  : "), d.CorrelationId,
					color.GreenString("# Reply-to        : "), d.ReplyTo,
					color.GreenString("# Headers         : "), d.Headers,
					color.GreenString("# Body            : "), d.Body)
			} else {
				count++
				fmt.Printf("\033[1A\033[K")
				log.Printf("💾 Consumed %s messages. To exit press %s", color.GreenString("%d", count), color.YellowString("CTRL+C"))
			}
		},
	}
	log.Printf("⏳ Waiting for messages. To exit press %s", color.YellowString("CTRL+C"))
	return consume.Runner{}.Run(ctx, consume.Spec{
		URL: rawURL, Insecure: insecure, Exchanges: exchanges,
		Queue: queue, Store: store, Silent: silent,
	}, hooks)
}

func lifecycleCommand(name, usage string) *cli.Command {
	return &cli.Command{
		Name:      name,
		Usage:     usage,
		ArgsUsage: "<task-id>",
		Action: func(_ context.Context, cmd *cli.Command) error {
			id, err := resolveTaskID(cmd)
			if err != nil {
				return err
			}
			task, err := ipc.NewClient().Action(id, name)
			if err != nil {
				return err
			}
			fmt.Printf("Task %s is now %s\n", shortID(task.ID), task.Status)
			return nil
		},
	}
}

func runList(_ context.Context, cmd *cli.Command) error {
	tasks, err := ipc.NewClient().List()
	if err != nil {
		return hintDaemon(err)
	}
	if cmd.Bool("json") {
		return json.NewEncoder(os.Stdout).Encode(api.TasksResponse{Tasks: tasks})
	}
	if len(tasks) == 0 {
		fmt.Println("No tasks. Start one with: coyote consume --url ... --exchange ...")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tURL\tEXCHANGES\tQUEUE\tSTATUS\tRECEIVED\tERROR")
	for _, t := range tasks {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
			shortID(t.ID), t.URL, formatExchanges(t.Exchanges), t.Queue,
			t.Status, t.Stats.ReceivedCount, t.Stats.Error)
	}
	return w.Flush()
}

func runRm(_ context.Context, cmd *cli.Command) error {
	id, err := resolveTaskID(cmd)
	if err != nil {
		return err
	}
	client := ipc.NewClient()
	task, err := client.Get(id)
	if err != nil {
		return hintDaemon(err)
	}
	deleteQueue := cmd.Bool("delete-queue")
	if !deleteQueue && !cmd.Bool("keep-queue") && !cmd.Bool("yes") && task.QueueExplicit {
		deleteQueue = askDeleteQueue(task.Queue)
	}
	removed, err := client.Delete(id, deleteQueue, cmd.Bool("purge"))
	if err != nil {
		return err
	}
	fmt.Printf("Removed task %s\n", shortID(removed.ID))
	if removed.Stats.Error != "" {
		fmt.Printf("warning: %s\n", removed.Stats.Error)
	}
	return nil
}

func runLogs(_ context.Context, cmd *cli.Command) error {
	id, err := resolveTaskID(cmd)
	if err != nil {
		return err
	}
	lines, err := ipc.NewClient().Logs(id, int(cmd.Int("tail")))
	if err != nil {
		return hintDaemon(err)
	}
	for _, l := range lines {
		fmt.Println(l)
	}
	return nil
}

func runFetch(_ context.Context, cmd *cli.Command) error {
	id, err := resolveTaskID(cmd)
	if err != nil {
		return err
	}
	filter := api.MessageFilter{
		Exchange:      cmd.String("exchange"),
		RoutingKey:    cmd.String("routing-key"),
		CorrelationID: cmd.String("correlation-id"),
		ReplyTo:       cmd.String("reply-to"),
		Headers:       cmd.String("headers"),
		Body:          cmd.String("body"),
	}
	resp, err := ipc.NewClient().Messages(id, api.MessagesQuery{
		Limit:  int(cmd.Int("limit")),
		Offset: int(cmd.Int("offset")),
		Filter: filter,
	})
	if err != nil {
		return hintDaemon(err)
	}
	if active := filter.Active(); len(active) > 0 {
		fmt.Printf("Filter: %s\n", strings.Join(active, ", "))
	}
	if out := cmd.String("store"); out != "" {
		data, err := json.MarshalIndent(resp.Messages, "", "  ")
		if err != nil {
			return failed.Because("failed to encode messages:", err)
		}
		if err := os.WriteFile(out, data, 0o600); err != nil {
			return failed.Because("failed to write store file:", err)
		}
		fmt.Printf("Wrote %d of %d messages to %s\n", len(resp.Messages), resp.Total, out)
		return nil
	}
	if cmd.Bool("json") {
		return json.NewEncoder(os.Stdout).Encode(resp)
	}
	if len(resp.Messages) == 0 {
		fmt.Println("No messages stored.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTIMESTAMP\tEXCHANGE\tROUTING-KEY\tCORRELATION-ID\tBODY")
	for _, m := range resp.Messages {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n",
			m.ID, m.Timestamp, m.Exchange, m.RoutingKey, m.CorrelationID, truncate(m.Body, 80))
	}
	fmt.Fprintf(w, "\nShowing %d of %d messages.\n", len(resp.Messages), resp.Total)
	return w.Flush()
}

func runDaemonStart(_ context.Context, cmd *cli.Command) error {
	if cmd.Bool("foreground") {
		return daemon.Serve()
	}
	if err := ipc.EnsureRunning(); err != nil {
		return err
	}
	fmt.Println("Daemon is running.")
	return nil
}

func runDaemonStop(_ context.Context, _ *cli.Command) error {
	if !ipc.Probe() {
		fmt.Println("Daemon is not running.")
		return nil
	}
	if err := ipc.NewClient().Shutdown(); err != nil {
		return err
	}
	fmt.Println("Daemon stopped.")
	return nil
}

func runDaemonStatus(_ context.Context, _ *cli.Command) error {
	if !ipc.Probe() {
		fmt.Println("Daemon is not running.")
		return nil
	}
	tasks, err := ipc.NewClient().List()
	if err != nil {
		return err
	}
	fmt.Printf("Daemon is running with %d task(s).\n", len(tasks))
	return nil
}

// resolveTaskID accepts a full ID or a unique prefix.
func resolveTaskID(cmd *cli.Command) (string, error) {
	arg := strings.TrimSpace(cmd.Args().First())
	if arg == "" {
		if cmd.Args().Len() == 0 {
			// urfave may put the first arg differently when ArgsUsage is set
			arg = strings.TrimSpace(cmd.Args().Get(0))
		}
		if arg == "" {
			return "", failed.Because("task id is required", nil)
		}
	}
	tasks, err := ipc.NewClient().List()
	if err != nil {
		return "", hintDaemon(err)
	}
	if _, ok := findTask(tasks, arg); ok {
		return arg, nil
	}
	var matches []api.Task
	for _, t := range tasks {
		if strings.HasPrefix(t.ID, arg) {
			matches = append(matches, t)
		}
	}
	switch len(matches) {
	case 0:
		return "", failed.Because(fmt.Sprintf("no task matches %q", arg), nil)
	case 1:
		return matches[0].ID, nil
	default:
		return "", failed.Because(fmt.Sprintf("%q is ambiguous (%d matches)", arg, len(matches)), nil)
	}
}

func findTask(tasks []api.Task, id string) (api.Task, bool) {
	for _, t := range tasks {
		if t.ID == id {
			return t, true
		}
	}
	return api.Task{}, false
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func formatExchanges(m map[string]string) string {
	parts := make([]string, 0, len(m))
	for k, v := range m {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func hintDaemon(err error) error {
	if !ipc.Probe() {
		return failed.Because("daemon is not running (start one with 'coyote consume ...')", err)
	}
	return err
}

func askDeleteQueue(queueName string) bool {
	choices := []choose.Choice{
		{Text: "no", Note: "Keeps the queue and all messages in it"},
		{Text: "yes", Note: "Deletes the queue and all messages in it"},
	}
	id, err := prompt.
		New().
		Ask(fmt.Sprintf("Do you want to delete the persistent interceptor queue %s?", color.YellowString(queueName))).
		AdvancedChoose(choices)
	if err != nil {
		log.Printf("prompt failed, keeping queue: %v", err)
		return false
	}
	return id == "yes"
}
