package vfx

import "sync/atomic"

// NewAnimationSequence defines a builder for building a animation frame.
func NewAnimationSequence(stat Stats, s ...Sequence) Frame {
	as := AnimationSequence{sequences: s, stat: stat}
	return &as
}

// AnimationSequence defines a set of sequences that operate on the behaviour of
// a dom element or lists of dom.elements.
type AnimationSequence struct {
	sequences      SequenceList
	stat           Stats
	inited         int64
	done           int64
	completedFrame int64
	iniWriters     DeferWriters
}

// IsOver returns true/false if the animation is done.
func (f *AnimationSequence) IsOver() bool {
	if f.Stats().Loop() {
		return false
	}

	return atomic.LoadInt64(&f.done) > 1
}

// End allows forcing a stop to an animation frame.
func (f *AnimationSequence) End() {
	atomic.StoreInt64(&f.done, 1)
}

// Inited returns true/false if the frame has begun.
func (f *AnimationSequence) Inited() bool {
	return atomic.LoadInt64(&f.inited) > 1
}

// Init calls the initialization writers for each sequence, returning their
// respective initialization writers if any to be runned on the first loop.
func (f *AnimationSequence) Init() DeferWriters {
	if atomic.LoadInt64(&f.inited) > 1 {
		return f.iniWriters
	}

	var writers DeferWriters

	// Collect all writers from each sequence with in the frame.
	for _, seq := range f.sequences {
		writers = append(writers, seq.Init(f.Stats()))
	}

	atomic.StoreInt64(&f.inited, 1)
	f.iniWriters = append(f.iniWriters, writers...)
	return writers
}

// Sync allows the frame to check and perform any update to its operation.
func (f *AnimationSequence) Sync() {
	if f.Stats().IsDone() {

		// Set the completedFrame to one to indicate the frame has completed a full
		// first set animation(transition+reverse transition) of its sequences.
		atomic.StoreInt64(&f.completedFrame, 1)

		if f.Stats().Loop() {
			f.stat = f.stat.Clone()
			return
		}

		f.End()
	}
}

// Phase defines the frame phase, to allow optimization options by the gameloop.
func (f *AnimationSequence) Phase() FramePhase {
	if atomic.LoadInt64(&f.completedFrame) > 0 {
		return OPTIMISEPHASE
	}

	return STARTPHASE
}

// Stats return the frame internal stats.
func (f *AnimationSequence) Stats() Stats {
	return f.stat
}

// Sequence builds the lists of writers from each sequence item within
// the frame sequence lists.
func (f *AnimationSequence) Sequence() DeferWriters {
	var writers DeferWriters

	if f.Stats().Optimized() {
		if f.Stats().CompletedFirstTransition() {
			ct := f.Stats().CurrentIteration()
			return GetWriterCache().Writers(f, ct)
		}
	}

	// Collect all writers from each sequence with in the frame.
	for _, seq := range f.sequences {
		// If the sequence has finished its rounds, then skip it.
		if seq.IsDone() {
			continue
		}

		writers = append(writers, seq.Next(f.Stats()))
	}

	return writers
}
