package demo

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Renderer draws the mosaic grid and sidebar in the terminal using ANSI truecolor.
type Renderer struct {
	Config     *MosaicConfig
	LedgerPath string
	RootDir    string

	// grid state: RGB per tile, [0,0,0] means unset
	mu    sync.Mutex
	tiles [][3]byte
	done  int
	start time.Time

	// event log for sidebar
	events []string

	// keyboard action channel
	Actions chan byte
}

// NewRenderer creates a renderer for the given config and ledger path.
func NewRenderer(cfg *MosaicConfig, ledgerPath, rootDir string) *Renderer {
	return &Renderer{
		Config:     cfg,
		LedgerPath: ledgerPath,
		RootDir:    rootDir,
		tiles:      make([][3]byte, cfg.TotalTiles()),
		start:      time.Now(),
		Actions:    make(chan byte, 16),
	}
}

// Start runs the renderer loop at the configured FPS.
// It tails the ledger file and redraws the grid each frame.
func (r *Renderer) Start(ctx context.Context) {
	// Start ledger tailer in background
	go r.tailLedger(ctx)

	// Start keyboard reader if terminal
	if isTerminal(terminalFd()) {
		go r.readKeys(ctx)
	}

	interval := time.Second / time.Duration(r.Config.FPS)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Hide cursor
	fmt.Print("\033[?25l")

	for {
		select {
		case <-ctx.Done():
			// Show cursor, reset
			fmt.Print("\033[?25h\033[0m")
			return
		case <-ticker.C:
			r.draw()
		}
	}
}

// AddEvent adds a sidebar event message.
func (r *Renderer) AddEvent(msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, msg)
	if len(r.events) > 8 {
		r.events = r.events[len(r.events)-8:]
	}
}

// Done returns the number of tiles filled so far.
func (r *Renderer) Done() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.done
}

func (r *Renderer) tailLedger(ctx context.Context) {
	var offset int64

	for {
		if ctx.Err() != nil {
			return
		}

		f, err := os.Open(r.LedgerPath) //nolint:gosec // path is controlled
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			_ = f.Close()
			time.Sleep(50 * time.Millisecond)
			continue
		}

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var entry LedgerEntry
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				continue
			}
			total := r.Config.TotalTiles()
			if entry.Index >= 0 && entry.Index < total {
				r.mu.Lock()
				r.tiles[entry.Index] = entry.RGB
				r.done++
				r.mu.Unlock()
			}
		}

		newOffset, _ := f.Seek(0, io.SeekCurrent)
		offset = newOffset
		_ = f.Close()

		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Millisecond):
		}
	}
}

func (r *Renderer) readKeys(ctx context.Context) {
	fd := terminalFd()
	oldState, err := makeRaw(fd)
	if err != nil {
		return
	}
	defer restoreTerminal(fd, oldState)

	buf := make([]byte, 1)
	for {
		if ctx.Err() != nil {
			return
		}
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			return
		}
		select {
		case r.Actions <- buf[0]:
		default:
		}
	}
}

func (r *Renderer) draw() {
	r.mu.Lock()
	tiles := make([][3]byte, len(r.tiles))
	copy(tiles, r.tiles)
	done := r.done
	events := make([]string, len(r.events))
	copy(events, r.events)
	r.mu.Unlock()

	gx := r.Config.GridX
	gy := r.Config.GridY
	total := r.Config.TotalTiles()

	var buf strings.Builder
	buf.Grow(gx*gy*30 + 500) // rough estimate

	// Cursor home
	buf.WriteString("\033[H")

	// Header
	elapsed := time.Since(r.start).Truncate(time.Millisecond)
	rate := float64(0)
	if elapsed.Seconds() > 0 {
		rate = float64(done) / elapsed.Seconds()
	}
	header := fmt.Sprintf(" LOKT MOSAIC  %d/%d (%.0f/s)  %s  mode=%s",
		done, total, rate, elapsed, r.Config.Mode)
	buf.WriteString("\033[1;37;44m")
	buf.WriteString(header)
	// Pad to fill line
	if len(header) < gx*2+30 {
		buf.WriteString(strings.Repeat(" ", gx*2+30-len(header)))
	}
	buf.WriteString("\033[0m\n")

	// Grid + sidebar
	sidebarLines := buildSidebar(done, total, rate, elapsed, events, r.Config)

	for y := 0; y < gy; y++ {
		for x := 0; x < gx; x++ {
			idx := y*gx + x
			c := tiles[idx]
			if c == [3]byte{0, 0, 0} {
				// Unset tile — dark gray
				buf.WriteString("\033[48;2;30;30;30m  \033[0m")
			} else {
				buf.WriteString(fmt.Sprintf("\033[48;2;%d;%d;%dm  \033[0m", c[0], c[1], c[2]))
			}
		}

		// Sidebar column (right of grid)
		buf.WriteString("  ")
		if y < len(sidebarLines) {
			buf.WriteString(sidebarLines[y])
		}

		// Clear rest of line
		buf.WriteString("\033[K\n")
	}

	// Clear remaining lines below grid
	for i := 0; i < 3; i++ {
		buf.WriteString("\033[K\n")
	}

	fmt.Print(buf.String())
}

func buildSidebar(done, total int, rate float64, elapsed time.Duration, events []string, cfg *MosaicConfig) []string {
	pct := float64(0)
	if total > 0 {
		pct = float64(done) / float64(total) * 100
	}
	eta := ""
	if rate > 0 && done < total {
		remaining := float64(total-done) / rate
		eta = fmt.Sprintf("  ETA %s", time.Duration(remaining*float64(time.Second)).Truncate(time.Second))
	}

	lines := []string{
		fmt.Sprintf("\033[1mProgress\033[0m %d/%d  %.1f%%%s", done, total, pct, eta),
		fmt.Sprintf("\033[1mRate\033[0m     %.1f tiles/sec", rate),
		fmt.Sprintf("\033[1mElapsed\033[0m  %s", elapsed.Truncate(time.Second)),
		fmt.Sprintf("\033[1mWorkers\033[0m  %d", cfg.Workers),
		fmt.Sprintf("\033[1mGrid\033[0m     %dx%d  seed=%d", cfg.GridX, cfg.GridY, cfg.Seed),
		fmt.Sprintf("\033[1mMode\033[0m     %s", cfg.Mode),
		"",
		"\033[1mKeys\033[0m  f=freeze u=unfreeze k=kill q=quit",
		"",
	}

	if len(events) > 0 {
		lines = append(lines, "\033[1mEvents\033[0m")
		for _, e := range events {
			lines = append(lines, "  "+e)
		}
	}

	return lines
}
