package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fyne.io/systray"
)

func runMenuBar(ctx context.Context, tuning runtimeTuning) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("spank requires root privileges for accelerometer access, run with: sudo spank --menubar")
	}

	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	accelRing, err := startSensor(ctx)
	if err != nil {
		return err
	}
	if accelRing == nil {
		return nil
	}
	defer accelRing.Close()
	defer accelRing.Unlink()

	// modeRestartCh signals the listen loop to restart with a new pack.
	modeRestartCh := make(chan struct{}, 1)

	var runErr error
	systray.Run(func() {
		systray.SetTitle("👋")
		systray.SetTooltip("spank — slap detector")

		// ── Enabled toggle ──────────────────────────────────────────────
		mEnabled := systray.AddMenuItemCheckbox("Enabled", "Toggle slap detection on/off", true)
		systray.AddSeparator()

		// ── Mode ────────────────────────────────────────────────────────
		mModeHeader := systray.AddMenuItem("Mode", "")
		mModeHeader.Disable()
		type modeEntry struct {
			label   string
			setSex  bool
			setHalo bool
			setLiz  bool
		}
		modes := []modeEntry{
			{"  Pain (default)", false, false, false},
			{"  Sexy", true, false, false},
			{"  Halo", false, true, false},
			{"  Lizard", false, false, true},
		}
		activeModeIdx := 0
		if sexyMode {
			activeModeIdx = 1
		} else if haloMode {
			activeModeIdx = 2
		} else if lizardMode {
			activeModeIdx = 3
		}
		modeItems := make([]*systray.MenuItem, len(modes))
		for i, m := range modes {
			modeItems[i] = systray.AddMenuItemCheckbox(m.label, "", i == activeModeIdx)
		}
		systray.AddSeparator()

		// ── Jerk threshold ──────────────────────────────────────────────
		mJerkHeader := systray.AddMenuItem("Jerk Threshold", "Rate of acceleration change required to count as a slap")
		mJerkHeader.Disable()
		type floatPreset struct {
			label string
			value float64
		}
		jerkPresets := []floatPreset{
			{"  Off (no filter)", 0},
			{"  Low (20 g/s)", 20},
			{"  Medium (50 g/s)", 50},
			{"  High (100 g/s)", 100},
		}
		activeJerkIdx := 2
		for i, p := range jerkPresets {
			if p.value == minJerk {
				activeJerkIdx = i
			}
		}
		jerkItems := make([]*systray.MenuItem, len(jerkPresets))
		for i, p := range jerkPresets {
			jerkItems[i] = systray.AddMenuItemCheckbox(p.label, "", i == activeJerkIdx)
		}
		systray.AddSeparator()

		// ── Min Amplitude ────────────────────────────────────────────────
		mAmpHeader := systray.AddMenuItem("Min Amplitude", "Minimum g-force to trigger a response")
		mAmpHeader.Disable()
		ampPresets := []floatPreset{
			{"  Subtle (0.05 g)", 0.05},
			{"  Normal (0.10 g)", 0.10},
			{"  Firm (0.20 g)", 0.20},
			{"  Hard (0.40 g)", 0.40},
		}
		activeAmpIdx := 1
		for i, p := range ampPresets {
			if p.value == minAmplitude {
				activeAmpIdx = i
			}
		}
		ampItems := make([]*systray.MenuItem, len(ampPresets))
		for i, p := range ampPresets {
			ampItems[i] = systray.AddMenuItemCheckbox(p.label, "", i == activeAmpIdx)
		}
		systray.AddSeparator()

		// ── Cooldown ─────────────────────────────────────────────────────
		mCoolHeader := systray.AddMenuItem("Cooldown", "Minimum time between responses")
		mCoolHeader.Disable()
		type intPreset struct {
			label string
			value int
		}
		coolPresets := []intPreset{
			{"  Fast (250 ms)", 250},
			{"  Normal (500 ms)", 500},
			{"  Default (750 ms)", 750},
			{"  Relaxed (1500 ms)", 1500},
		}
		activeCoolIdx := 2
		for i, p := range coolPresets {
			if p.value == cooldownMs {
				activeCoolIdx = i
			}
		}
		coolItems := make([]*systray.MenuItem, len(coolPresets))
		for i, p := range coolPresets {
			coolItems[i] = systray.AddMenuItemCheckbox(p.label, "", i == activeCoolIdx)
		}
		systray.AddSeparator()

		// ── Playback Speed ───────────────────────────────────────────────
		mSpeedHeader := systray.AddMenuItem("Playback Speed", "Speed multiplier for audio playback")
		mSpeedHeader.Disable()
		speedPresets := []floatPreset{
			{"  Slow (0.75×)", 0.75},
			{"  Normal (1×)", 1.0},
			{"  Fast (1.5×)", 1.5},
			{"  Turbo (2×)", 2.0},
		}
		activeSpeedIdx := 1
		for i, p := range speedPresets {
			if p.value == speedRatio {
				activeSpeedIdx = i
			}
		}
		speedItems := make([]*systray.MenuItem, len(speedPresets))
		for i, p := range speedPresets {
			speedItems[i] = systray.AddMenuItemCheckbox(p.label, "", i == activeSpeedIdx)
		}
		systray.AddSeparator()

		// ── Toggles ──────────────────────────────────────────────────────
		mVolScale := systray.AddMenuItemCheckbox("Volume Scaling", "Louder for harder hits", volumeScaling)
		systray.AddSeparator()

		// ── Quit ─────────────────────────────────────────────────────────
		mQuit := systray.AddMenuItem("Quit spank", "")

		// ── Listen loop (restartable for mode changes) ───────────────────
		var listenCancel context.CancelFunc
		startListening := func() {
			if listenCancel != nil {
				listenCancel()
			}
			pack, err := buildPack()
			if err != nil {
				fmt.Fprintf(os.Stderr, "spank: %v\n", err)
				return
			}
			listenCtx, lcancel := context.WithCancel(ctx)
			listenCancel = lcancel
			go func() {
				if err := listenForSlaps(listenCtx, pack, accelRing, tuning); err != nil &&
					err != context.Canceled {
					fmt.Fprintf(os.Stderr, "spank: %v\n", err)
					runErr = err
					systray.Quit()
				}
			}()
		}
		startListening()

		// Drain modeRestartCh and restart when signalled
		go func() {
			for range modeRestartCh {
				startListening()
			}
		}()

		// Watch for outer context cancellation
		go func() {
			<-ctx.Done()
			systray.Quit()
		}()

		// ── Click handlers ───────────────────────────────────────────────

		go func() {
			for range mEnabled.ClickedCh {
				pausedMu.Lock()
				if mEnabled.Checked() {
					mEnabled.Uncheck()
					paused = true
				} else {
					mEnabled.Check()
					paused = false
				}
				pausedMu.Unlock()
			}
		}()

		setMode := func(idx int) {
			for j, item := range modeItems {
				if j == idx {
					item.Check()
				} else {
					item.Uncheck()
				}
			}
			sexyMode = modes[idx].setSex
			haloMode = modes[idx].setHalo
			lizardMode = modes[idx].setLiz
			select {
			case modeRestartCh <- struct{}{}:
			default:
			}
		}
		for i, item := range modeItems {
			i, item := i, item
			go func() {
				for range item.ClickedCh {
					setMode(i)
				}
			}()
		}

		setFloatRadio := func(items []*systray.MenuItem, idx int, target *float64, presets []floatPreset) {
			for j, s := range items {
				if j == idx {
					s.Check()
				} else {
					s.Uncheck()
				}
			}
			*target = presets[idx].value
		}

		for i, item := range jerkItems {
			i, item := i, item
			go func() {
				for range item.ClickedCh {
					setFloatRadio(jerkItems, i, &minJerk, jerkPresets)
				}
			}()
		}
		for i, item := range ampItems {
			i, item := i, item
			go func() {
				for range item.ClickedCh {
					setFloatRadio(ampItems, i, &minAmplitude, ampPresets)
				}
			}()
		}
		for i, item := range speedItems {
			i, item := i, item
			go func() {
				for range item.ClickedCh {
					setFloatRadio(speedItems, i, &speedRatio, speedPresets)
				}
			}()
		}

		for i, item := range coolItems {
			i, item := i, item
			go func() {
				for range item.ClickedCh {
					for j, s := range coolItems {
						if j == i {
							s.Check()
						} else {
							s.Uncheck()
						}
					}
					cooldownMs = coolPresets[i].value
					tuning.cooldown = time.Duration(cooldownMs) * time.Millisecond
				}
			}()
		}

		go func() {
			for range mVolScale.ClickedCh {
				if mVolScale.Checked() {
					mVolScale.Uncheck()
					volumeScaling = false
				} else {
					mVolScale.Check()
					volumeScaling = true
				}
			}
		}()

		go func() {
			<-mQuit.ClickedCh
			cancel()
			systray.Quit()
		}()

	}, func() {
		cancel()
	})

	return runErr
}
