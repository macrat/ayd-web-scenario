package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/macrat/ayd/lib-ayd"
	"github.com/spf13/pflag"
	"github.com/yuin/gopher-lua"
)

var (
	Version = "HEAD"
	Commit  = "UNKNOWN"
)

func StartBrowser(ctx context.Context, debuglog *ayd.Logger) (context.Context, context.CancelFunc) {
	var opts []chromedp.ContextOption
	if debuglog != nil {
		opts = append(
			opts,
			chromedp.WithLogf(func(s string, args ...any) {
				debuglog.Healthy(fmt.Sprintf(s, args), map[string]any{
					"level": "log",
				})
			}),
			chromedp.WithDebugf(func(s string, args ...any) {
				debuglog.Healthy(fmt.Sprintf(s, args), map[string]any{
					"level": "debug",
				})
			}),
			chromedp.WithErrorf(func(s string, args ...any) {
				debuglog.Failure(fmt.Sprintf(s, args), map[string]any{
					"level": "error",
				})
			}),
		)
	}
	ctx, cancel := chromedp.NewContext(ctx, opts...)
	chromedp.Run(ctx)
	return ctx, cancel
}

func NewContext(debuglog *ayd.Logger) (context.Context, context.CancelFunc) {
	ctx, cancel1 := context.WithTimeout(context.Background(), time.Hour)
	ctx, cancel2 := chromedp.NewExecAllocator(ctx, chromedp.DefaultExecAllocatorOptions[:]...)
	ctx, cancel3 := StartBrowser(ctx, debuglog)

	return ctx, func() {
		cancel3()
		cancel2()
		cancel1()
	}
}

func NewLuaState(ctx context.Context, logger *Logger, s *Storage) *lua.LState {
	L := lua.NewState()

	RegisterLogger(L, logger)
	RegisterElementsArrayType(ctx, L)
	RegisterElementType(ctx, L)
	RegisterTabType(ctx, L, s)
	RegisterTime(L)

	return L
}

func RunWebScenario(target *ayd.URL, debug bool) ayd.Record {
	timestamp := time.Now()

	logger := &Logger{Debug: debug, Status: ayd.StatusHealthy}

	baseDir := os.Getenv("WEBSCENARIO_ARTIFACT_DIR")
	storage, err := NewStorage(baseDir, target.Opaque, timestamp)
	if err != nil {
		return ayd.Record{
			Time:    timestamp,
			Status:  ayd.StatusFailure,
			Message: err.Error(),
		}
	}

	var browserlog *ayd.Logger
	if debug {
		f, err := storage.Open("browser.log")
		if err != nil {
			return ayd.Record{
				Time:    timestamp,
				Status:  ayd.StatusFailure,
				Message: err.Error(),
			}
		}
		defer f.Close()
		l := ayd.NewLoggerWithWriter(f, target)
		browserlog = &l
	}
	ctx, cancel := NewContext(browserlog)
	defer cancel()

	L := NewLuaState(ctx, logger, storage)
	defer L.Close()

	stime := time.Now()
	err = L.DoFile(target.Opaque)
	latency := time.Since(stime)

	if err != nil {
		var apierr *lua.ApiError
		if errors.As(err, &apierr) {
			logger.SetExtra("error", apierr.Object.String())
			logger.SetExtra("trace", apierr.StackTrace)
		} else {
			logger.SetExtra("error", err.Error())
		}
		logger.Status = ayd.StatusFailure
	}

	if xs := storage.Artifacts(); len(xs) > 0 {
		logger.SetExtra("artifacts", xs)
	}

	r := logger.AsRecord()
	r.Time = timestamp
	r.Latency = latency
	return r
}

func main() {
	flags := pflag.NewFlagSet("ayd-web-scenario-plugin", pflag.ContinueOnError)
	debugMode := flags.Bool("debug", false, "enable debug mode.")
	showVersion := flags.BoolP("version", "v", false, "show version and exit.")
	showHelp := flags.BoolP("help", "h", false, "show help message and exit.")

	if err := flags.Parse(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintf(os.Stderr, "\nPlease see `%s -h` for more information.\n", os.Args[0])
		os.Exit(2)
	}
	switch {
	case *showVersion:
		fmt.Printf("Ayd WebScenaro plugin %s (%s)\n", Version, Commit)
		return
	case *showHelp || len(flags.Args()) != 1:
		fmt.Println("$ ayd-web-scenario-plugin [OPTIONS] TARGET_URL\n\nOptions:")
		flags.PrintDefaults()
		return
	}

	target, err := ayd.ParseURL(flags.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintf(os.Stderr, "\nPlease see `%s -h` for more information.\n", os.Args[0])
		os.Exit(2)
	}

	r := RunWebScenario(target, *debugMode)
	ayd.NewLogger(target).Print(r)
}
