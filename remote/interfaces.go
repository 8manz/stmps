package remote

type ControlledPlayer interface {
	// Returns true if a seek is currently in progress.
	IsSeeking() (bool, error)
	IsPaused() (bool, error)
	IsPlaying() (bool, error)

	// Registers a callback which is invoked when the player transitions to the Paused state.
	OnPaused(cb func())

	// Registers a callback which is invoked when the player transitions to the Stopped state.
	OnStopped(cb func())

	// Registers a callback which is invoked when the player transitions to the Playing state.
	OnPlaying(cb func())

	// Registers a callback which is invoked whenever a seek event occurs.
	OnSeek(cb func())

	OnSongChange(func(track TrackInterface))

	GetTimePos() float64

	Play() error
	Pause() error
	Stop() error
	SeekAbsolute(float64) error
	NextTrack() error
	PreviousTrack() error

	SetVolume(percentValue int) error
}

type TrackInterface interface {
	GetArtist() string
	GetTitle() string
	GetDuration() int

	// something like ID != ""
	IsValid() bool
}
