// Copyright 2023 The STMP Authors
// SPDX-License-Identifier: GPL-3.0-or-later

package mpvplayer

import (
	"errors"
	"strconv"

	"github.com/wildeyedskies/go-mpv/mpv"
	"github.com/wildeyedskies/stmp/logger"
	"github.com/wildeyedskies/stmp/remote"
)

type PlayerQueue []QueueItem

type Player struct {
	instance      *mpv.Mpv
	mpvEvents     chan *mpv.Event
	eventConsumer EventConsumer
	queue         PlayerQueue
	logger        logger.LoggerInterface

	replaceInProgress bool
	stopped           bool
}

var _ remote.ControlledPlayer = (*Player)(nil)

func NewPlayer(logger logger.LoggerInterface) (player *Player, err error) {
	m := mpv.Create()

	// cargo-cult what supersonic does
	if err = m.SetOptionString("audio-display", "no"); err != nil {
		return
	}
	if err = m.SetOptionString("video", "no"); err != nil {
		return
	}
	if err = m.SetOptionString("terminal", "no"); err != nil {
		return
	}
	if err = m.SetOptionString("demuxer-max-bytes", "30MiB"); err != nil {
		return
	}
	if err = m.SetOptionString("audio-client-name", "stmp"); err != nil {
		return
	}

	if err = m.Initialize(); err != nil {
		return
	}

	player = &Player{
		instance:          m,
		mpvEvents:         make(chan *mpv.Event),
		eventConsumer:     nil, // must be set by calling RegisterEventConsumer()
		queue:             make([]QueueItem, 0),
		logger:            logger,
		replaceInProgress: false,
		stopped:           true,
	}

	go player.mpvEngineEventHandler(m)
	return
}

func (p *Player) mpvEngineEventHandler(instance *mpv.Mpv) {
	for {
		evt := instance.WaitEvent(1)
		p.mpvEvents <- evt
	}
}

func (p *Player) Quit() {
	p.mpvEvents <- nil
	p.instance.TerminateDestroy()
}

func (p *Player) RegisterEventConsumer(consumer EventConsumer) {
	p.eventConsumer = consumer
}

func (p *Player) PlayNextTrack() error {
	if len(p.queue) >= 1 {
		// advance queue if any tracks left
		p.queue = p.queue[1:]

		if len(p.queue) > 0 {
			// replace currently playing song with next song
			if loaded, err := p.IsSongLoaded(); err != nil {
				p.logger.PrintError("PlayNextTrack", err)
			} else if loaded {
				p.replaceInProgress = true
				if err := p.temporaryStop(); err != nil {
					p.logger.PrintError("temporaryStop", err)
				}
				return p.instance.Command([]string{"loadfile", p.queue[0].Uri})
			}
		} else {
			// stop with empty queue
			if err := p.Stop(); err != nil {
				p.logger.PrintError("Stop", err)
			}
		}
	} else {
		// queue empty
		if err := p.Stop(); err != nil {
			p.logger.PrintError("Stop", err)
		}
	}
	return nil
}

func (p *Player) PlayUri(id string, uri string, title string, artist string, duration int) error {
	p.queue = []QueueItem{{id, uri, title, artist, duration}}
	p.replaceInProgress = true
	if ip, e := p.IsPaused(); ip && e == nil {
		if err := p.Pause(); err != nil {
			p.logger.PrintError("Pause", err)
		}
	}
	return p.instance.Command([]string{"loadfile", uri})
}

func (p *Player) Stop() error {
	p.logger.Printf("stopping (user)")
	p.stopped = true
	return p.instance.Command([]string{"stop"})
}

func (p *Player) temporaryStop() error {
	return p.instance.Command([]string{"stop"})
}

func (p *Player) IsSongLoaded() (bool, error) {
	idle, err := p.instance.GetProperty("idle-active", mpv.FORMAT_FLAG)
	return !idle.(bool), err
}

func (p *Player) IsPaused() (bool, error) {
	pause, err := p.instance.GetProperty("pause", mpv.FORMAT_FLAG)
	return pause.(bool), err
}

func (p *Player) IsPlaying() (playing bool, err error) {
	if idle, err := p.instance.GetProperty("idle-active", mpv.FORMAT_FLAG); err != nil {
	} else if paused, err := p.instance.GetProperty("pause", mpv.FORMAT_FLAG); err != nil {
	} else {
		playing = !idle.(bool) && !paused.(bool)
	}
	return
}

func (p *Player) Test() {
	res, err := p.instance.GetProperty("idle-active", mpv.FORMAT_FLAG)
	p.logger.Printf("res %v err %v", res, err)
}

// Pause toggles playing music
// If a song is playing, it is paused. If a song is paused, playing resumes.
// If stopped, the song starts playing.
// The state after the toggle is returned, or an error.
func (p *Player) Pause() (err error) {
	loaded, err := p.IsSongLoaded()
	if err != nil {
		return
	}
	paused, err := p.IsPaused()
	if err != nil {
		return
	}

	if loaded && !p.stopped {
		// toggle pause if not stopped
		err = p.instance.Command([]string{"cycle", "pause"})
		if err != nil {
			p.logger.PrintError("cycle pause", err)
			return
		}
		paused = !paused

		currentSong := QueueItem{}
		if len(p.queue) > 0 {
			currentSong = p.queue[0]
		}

		if paused {
			p.sendGuiDataEvent(EventPaused, currentSong)
		} else {
			p.sendGuiDataEvent(EventUnpaused, currentSong)
		}
	} else {
		if len(p.queue) > 0 {
			currentSong := p.queue[0]
			err = p.instance.Command([]string{"loadfile", currentSong.Uri})
			if err != nil {
				p.logger.PrintError("loadfile", err)
				return
			}

			if p.stopped {
				p.stopped = false
				if err = p.instance.SetProperty("pause", mpv.FORMAT_FLAG, false); err != nil {
					p.logger.PrintError("setprop pause", err)
				}

				// mpv will send start file event which also sends the gui event
				//p.sendGuiDataEvent(EventPlaying, currentSong)
			} else {
				p.sendGuiDataEvent(EventUnpaused, currentSong)
			}
		} else {
			p.stopped = true
			p.sendGuiEvent(EventStopped)
		}
	}

	return
}

func (p *Player) SetVolume(percentValue int) error {
	if percentValue > 100 {
		percentValue = 100
	} else if percentValue < 0 {
		percentValue = 0
	}

	return p.instance.SetProperty("volume", mpv.FORMAT_INT64, percentValue)
}

func (p *Player) AdjustVolume(increment int) error {
	volume, err := p.instance.GetProperty("volume", mpv.FORMAT_INT64)
	if err != nil {
		return err
	}
	if volume == nil {
		return nil
	}

	return p.SetVolume(volume.(int) + increment)
}

func (p *Player) Volume() (int64, error) {
	volume, err := p.instance.GetProperty("volume", mpv.FORMAT_INT64)
	if err != nil {
		return -1, err
	}
	return volume.(int64), nil
}

func (p *Player) Seek(increment int) error {
	return p.instance.Command([]string{"seek", strconv.Itoa(increment)})
}

// accessed from gui context
func (p *Player) ClearQueue() {
	if err := p.Stop(); err != nil {
		p.logger.PrintError("Stop", err)
	}
	p.queue = make([]QueueItem, 0) // TODO mutex queue access
}

func (p *Player) DeleteQueueItem(index int) {
	// TODO mutex queue access
	if index >= len(p.queue) {
		p.logger.Printf("DeleteQueueItem bad index %d (len %d)", index, len(p.queue))
	} else if len(p.queue) > 1 {
		if index == 0 {
			if err := p.PlayNextTrack(); err != nil {
				p.logger.PrintError("PlayNextTrack", err)
			}
		} else {
			p.queue = append(p.queue[:index], p.queue[index+1:]...)
		}
	} else {
		p.ClearQueue()
	}
}

func (p *Player) AddToQueue(item *QueueItem) {
	p.queue = append(p.queue, *item)
}

func (p *Player) GetQueueItem(index int) (QueueItem, error) {
	if index < 0 || index >= len(p.queue) {
		return QueueItem{}, errors.New("invalid queue entry")
	}
	return p.queue[index], nil
}

func (p *Player) GetQueueCopy() PlayerQueue {
	cpy := make(PlayerQueue, len(p.queue))
	copy(cpy, p.queue)
	return cpy
}

// accessed from background context
func (p *Player) GetPlayingTrack() (QueueItem, error) {
	paused, err := p.IsPaused()
	if err != nil {
		return QueueItem{}, err
	}
	if paused {
		return QueueItem{}, errors.New("not playing")
	}

	if len(p.queue) == 0 { // TODO mutex queue access
		return QueueItem{}, errors.New("queue empty")
	}
	currentSong := p.queue[0]
	return currentSong, nil
}

func (p *Player) IsSeeking() (bool, error) {
	return false, nil
}

// Registers a callback which is invoked when the player transitions to the Paused state.
func (p *Player) OnPaused(cb func()) {}

// Registers a callback which is invoked when the player transitions to the Stopped state.
func (p *Player) OnStopped(cb func()) {}

// Registers a callback which is invoked when the player transitions to the Playing state.
func (p *Player) OnPlaying(cb func()) {}

// Registers a callback which is invoked whenever a seek event occurs.
func (p *Player) OnSeek(cb func()) {}

func (p *Player) OnSongChange(func(track remote.TrackInterface)) {}

func (p *Player) GetTimePos() float64 {
	return 0
}

func (p *Player) SeekAbsolute(float64) error {
	return nil
}

func (p *Player) Play() error {
	if isPlaying, err := p.IsPlaying(); err != nil {
		return err
	} else if !isPlaying {
		p.Pause()
	}
	return nil
}

func (p *Player) NextTrack() error {
	return p.PlayNextTrack()
}

func (p *Player) PreviousTrack() (err error) {
	if err = p.Stop(); err != nil {
		return
	}
	return p.Pause()
}
