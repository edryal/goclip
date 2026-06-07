package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
)

const applicationName string = "goclip"
const initialClass string = "com.github.edryal." + applicationName
const clipboardCommand string = "wl-copy"

type clipboardEntry struct {
	index string
	value string
}

func main() {
	initLogger()
	handleCmdLineArgs()
	enforceSingleInstance()

	app := gtk.NewApplication(initialClass, gio.ApplicationDefaultFlags)
	app.ConnectActivate(func() { activate(app) })
	if code := app.Run(os.Args); code > 0 {
		os.Exit(code)
	}
}

func initLogger() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)
}

func activate(app *gtk.Application) {
	mainWindow := gtk.NewApplicationWindow(app)
	applyMainWindowProperties(mainWindow)
	attachKeyboardShortcuts(mainWindow)
	startSocketListener(mainWindow)

	refreshList(mainWindow)
	mainWindow.SetVisible(true)
}

func handleCmdLineArgs() {
	toggle := flag.Bool("toggle", false, "toggle the visibility of a running instance")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%v - a Wayland clipboard history manager\n\n", applicationName)
		fmt.Fprintf(os.Stderr, "usage:\n")
		fmt.Fprintf(os.Stderr, "  %v           start the clipboard manager\n", applicationName)
		fmt.Fprintf(os.Stderr, "  %v --toggle  show or hide the running instance\n\n", applicationName)
		fmt.Fprintf(os.Stderr, "keybinds:\n")
		fmt.Fprintf(os.Stderr, "  ctrl+r     refresh clipboard history\n")
		fmt.Fprintf(os.Stderr, "  escape     hide the window\n\n")
		fmt.Fprintf(os.Stderr, "flags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *toggle {
		if err := sendToggleSignal(); err != nil {
			slog.Error("failed to send toggle signal", "app", applicationName, "err", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
}

func applyMainWindowProperties(mainWindow *gtk.ApplicationWindow) {
	mainWindow.SetTitle(applicationName)
	mainWindow.SetDefaultSize(700, 500)
	mainWindow.SetResizable(true)
}

func attachKeyboardShortcuts(mainWindow *gtk.ApplicationWindow) {
	keyController := gtk.NewEventControllerKey()
	keyController.ConnectKeyPressed(func(keyval, keycode uint, state gdk.ModifierType) bool {
		switch {
		case keyval == gdk.KEY_r && state&gdk.ControlMask != 0:
			refreshList(mainWindow)
			return true
		case keyval == gdk.KEY_Escape:
			mainWindow.SetVisible(false)
			return true
		}
		return false
	})
	mainWindow.AddController(keyController)
}

func refreshList(mainWindow *gtk.ApplicationWindow) {
	entries, err := readClipboardHistory()
	if err != nil {
		slog.Error("failed to refresh clipboard history", "err", err)
		return
	}
	mainWindow.SetChild(buildEntryList(entries))
}

func readClipboardHistory() ([]clipboardEntry, error) {
	cliphistCmd := exec.Command("cliphist", "list")
	cliphistOut, err := cliphistCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run cliphist: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(cliphistOut)), "\n")
	slog.Debug("cliphist output received", "lines", len(lines))

	entries := []clipboardEntry{}

	// "cliphist list" always outputs in this format: "<id>\t<100 char preview>"
	// if this ever fails the format has changed or output is corrupted
	for i, line := range lines {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			slog.Error("unexpected cliphist output format, skipping line",
				"line_index", i,
				"raw_line", line,
				"hint", "cliphist output format may have changed",
			)
			continue
		}

		slog.Debug("parsed entry", "index", parts[0], "preview", parts[1][:min(len(parts[1]), 40)])
		entries = append(entries, clipboardEntry{
			index: parts[0],
			value: parts[1],
		})
	}

	slog.Info("clipboard history loaded", "total_entries", len(entries))
	return entries, nil
}

func buildEntryList(entries []clipboardEntry) *gtk.ScrolledWindow {
	// ListBox works better than a Box filled with Buttons.
	// The normal Box had cursor hover offset issues and viewport jank.
	list := gtk.NewListBox()
	list.SetSelectionMode(gtk.SelectionSingle)

	for _, entry := range entries {
		label := gtk.NewLabel(entry.value)
		label.SetEllipsize(pango.EllipsizeEnd)
		label.SetMarginStart(8)
		label.SetMarginEnd(8)
		label.SetMarginTop(6)
		label.SetMarginBottom(6)

		// ListBoxRow instead of Buttons since Buttons have their own hover highlight
		// which stacks with ListBox's row highlight, causing a double highlight effect.
		row := gtk.NewListBoxRow()
		row.SetChild(label)
		list.Append(row)
	}

	list.ConnectRowActivated(func(row *gtk.ListBoxRow) {
		idx := row.Index()
		onClipboardEntryClicked(&entries[idx])
	})

	scroll := gtk.NewScrolledWindow()
	scroll.SetChild(list)
	scroll.SetVExpand(true)

	// Only vertical scrollbar since labels are ellipsized and horizontal scroll is never needed.
	scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)

	return scroll
}

func onClipboardEntryClicked(entry *clipboardEntry) {
	slog.Debug("entry clicked", "index", entry.index, "value", entry.value)

	decodeCmd := exec.Command("cliphist", "decode", entry.index)
	copyCmd := exec.Command(clipboardCommand)

	// equivalent to "cliphist decode <index> | wl-copy" in bash
	pipe, err := decodeCmd.StdoutPipe()
	if err != nil {
		slog.Error("failed to create decode pipe", "index", entry.index, "err", err)
		return
	}
	copyCmd.Stdin = pipe

	if err := copyCmd.Start(); err != nil {
		slog.Error("failed to start wl-copy", "err", err)
		return
	}

	if err := decodeCmd.Run(); err != nil {
		slog.Error("failed to run cliphist decode", "index", entry.index, "err", err)
		return
	}

	if err := copyCmd.Wait(); err != nil {
		slog.Error("failed to wait for wl-copy", "err", err)
		return
	}

	slog.Info("clipboard entry copied", "index", entry.index)
}

func socketPath() string {
	runDir := os.Getenv("XDG_RUNTIME_DIR")
	if runDir == "" {
		runDir = fmt.Sprintf("/tmp/user/%d", os.Getuid())
	}
	return filepath.Join(runDir, applicationName+".sock")
}

func lockFilePath() string {
	runDir := os.Getenv("XDG_RUNTIME_DIR")
	if runDir == "" {
		runDir = fmt.Sprintf("/tmp/user/%d", os.Getuid())
	}
	return filepath.Join(runDir, applicationName+".lock")
}

func sendToggleSignal() error {
	conn, err := net.Dial("unix", socketPath())
	if err != nil {
		return fmt.Errorf("could not connect to %v socket: %w", applicationName, err)
	}
	conn.Close()
	return nil
}

func startSocketListener(mainWindow *gtk.ApplicationWindow) {
	path := socketPath()
	os.Remove(path)

	listener, err := net.Listen("unix", path)
	if err != nil {
		slog.Error("failed to create socket listener", "path", path, "err", err)
		return
	}

	slog.Debug("socket listener started", "path", path)

	mainWindow.ConnectDestroy(func() {
		listener.Close()
		os.Remove(path)
		slog.Debug("socket listener stopped", "path", path)
	})

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
			slog.Debug("toggle signal received")
			glib.IdleAdd(func() {
				mainWindow.SetVisible(!mainWindow.IsVisible())
				if mainWindow.IsVisible() {
					mainWindow.Present()
				}
			})
		}
	}()
}

func enforceSingleInstance() {
	path := lockFilePath()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		slog.Error("failed to open lock file", "path", path, "err", err)
		os.Exit(1)
	}

	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		slog.Info("already running, use --toggle to show/hide", "app", applicationName)
		os.Exit(0)
	}

	// write PID into the lock file for easier debugging
	file.WriteString(fmt.Sprintf("%d\n", os.Getpid()))
	slog.Debug("lock acquired", "path", path, "pid", os.Getpid())
}
