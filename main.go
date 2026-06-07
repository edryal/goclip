package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
)

type clipboardEntry struct {
	index string
	value string
}

func main() {
	initLogger()

	app := gtk.NewApplication("com.github.edryal.goclip", gio.ApplicationDefaultFlags)
	app.ConnectActivate(func() { activate(app) })

	if code := app.Run(os.Args); code > 0 {
		os.Exit(code)
	}
}

func initLogger() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(logger)
}

func activate(app *gtk.Application) {
	mainWindow := gtk.NewApplicationWindow(app)
	mainWindow.SetTitle("goclip")
	mainWindow.SetDefaultSize(400, 300)
	mainWindow.SetResizable(true)

	entries, err := readClipboardHistory()
	if err != nil {
		panic(err)
	}

	scrolledWindow := buildEntryList(entries)
	mainWindow.SetChild(scrolledWindow)

	mainWindow.SetVisible(true)
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

	return entries, nil
}

func buildEntryList(entries []clipboardEntry) *gtk.ScrolledWindow {
	// ListBox works better than a Box filled with Buttons.
	// The normal Box had too many quirks related to the viewpoint becoming janky and the cursor's hover selection
	// was highlighting around 2-3 buttons lower than it should, not the botton it was actually on top of
	list := gtk.NewListBox()
	list.SetSelectionMode(gtk.SelectionSingle)

	for _, entry := range entries {
		label := gtk.NewLabel(entry.value)
		label.SetEllipsize(pango.EllipsizeEnd)
		label.SetMarginStart(8)
		label.SetMarginEnd(8)
		label.SetMarginTop(6)
		label.SetMarginBottom(6)

		// ListBoxRow instead of Buttons since yeah it makes sense and Buttons have their own highlighting
		// when hovered, which would layer/stack with ListBox's own hover highlighting of that row
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

	// Only vertical scrollbar since we ellipsize the labels anyways
	scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)

	return scroll
}

// TODO: when the user clicks on an entry, make it the most recent item in their clipboard
// most probably will run "cliphist decode <index>" and pipe the output to "wl-copy"
func onClipboardEntryClicked(entry *clipboardEntry) {
	slog.Debug("entry clicked", "index", entry.index, "value", entry.value)
}
