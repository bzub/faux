package vfx

import (
	"sync/atomic"

	"github.com/influx6/faux/fque"
	"github.com/influx6/faux/loop"
)

//==============================================================================

// FramePhase defines a animation phase type.
type FramePhase int

// const contains sets of Frame phase that identify the current frame animation
// phase.
const (
	NOPHASE FramePhase = iota
	STARTPHASE
	OPTIMISEPHASE
)

// Frame defines the interface for a animation sequence generator,
// it defines the sequence of a organized step for animation.
type Frame interface {
	End()
	Sync()
	Stats() Stats
	Inited() bool
	IsOver() bool
	Init(float64) DeferWriters
	Phase() FramePhase
	Sequence(float64) DeferWriters
	Cycles() int
	LastCycles() int
	OnBegin(func(Stats)) loop.Looper
	OnEnd(func(Stats)) loop.Looper
	OnProgress(func(Stats)) loop.Looper
}

// AnimationSequence defines a set of sequences that operate on the behaviour of
// a dom element or lists of dom.elements.
type AnimationSequence struct {
	sequences      SequenceList
	stoppers       []StoppableSequence
	stat           Stats
	inited         int64
	done           int64
	completedFrame int64
	iniWriters     DeferWriters
	selector       string
	elementals     Elementals
	totalCycles    int64
	lastCycle      int64
	writesOn       int64
	progress       fque.Qu
	begin          fque.Qu
	ended          fque.Qu
}

// NewAnimationSequence defines a builder for building a animation frame.
func NewAnimationSequence(selector string, stat Stats, s ...Sequence) Frame {
	as := AnimationSequence{
		selector:  selector,
		sequences: s,
		stat:      stat,
		progress:  fque.New(),
		begin:     fque.New(),
		ended:     fque.New(),
	}

	return &as
}

// IsOver returns true/false if the animation is done.
func (f *AnimationSequence) IsOver() bool {
	if f.Stats().Loop() {
		return false
	}

	return atomic.LoadInt64(&f.done) > 0
}

// OnProgress provides a callback hook to listen to progress of the animation,
// this is fired through out the duration of the animation.
func (f *AnimationSequence) OnProgress(fx func(Stats)) loop.Looper {
	return f.progress.Q(func() {
		fx(f.Stats())
	})
}

// OnBegin callbacks are fired once, at the beginning of an animation, even if
// the animation runs in a loop, it still will not be fired more than once.
func (f *AnimationSequence) OnBegin(fx func(Stats)) loop.Looper {
	return f.begin.Q(func() {
		fx(f.Stats())
	})
}

// OnEnd callbacks are fired once, at the end of an animation, if the animation
// the animation runs in a loop, it still will not be fired more than once at
// the end of the total loop count.
func (f *AnimationSequence) OnEnd(fx func(Stats)) loop.Looper {
	return f.ended.Q(func() {
		fx(f.Stats())
	})
}

// End allows forcing a stop to an animation frame.
func (f *AnimationSequence) End() {
	atomic.StoreInt64(&f.done, 1)
}

// Inited returns true/false if the frame has begun.
func (f *AnimationSequence) Inited() bool {
	return atomic.LoadInt64(&f.inited) > 0
}

// Init calls the initialization writers for each sequence, returning their
// respective initialization writers if any to be runned on the first loop.
func (f *AnimationSequence) Init(ms float64) DeferWriters {
	if atomic.LoadInt64(&f.inited) > 0 {
		return f.iniWriters
	}

	f.elementals = QuerySelectorAll(f.selector)

	var writers DeferWriters

	// Add the BeginWriting writer to set p execution reconciliation policy.
	writers = append(writers, &frameBeginWriter{f: f})

	if f.Stats().Delay() > 0 {
		writers = append(writers, &delayedWriter{
			ms: f.Stats().Delay(),
			f:  f,
		})
	}

	// Collect all writers from each sequence within the frame.
	for _, seq := range f.sequences {
		if ssq, ok := seq.(StoppableSequence); ok {
			f.stoppers = append(f.stoppers, ssq)
		}

		writers = append(writers, seq.Init(f.Stats(), f.elementals)...)
	}

	// Add the DoneWriting writer to setup execution reconciliation ending policy.
	writers = append(writers, &frameEndWriter{f: f})

	// If we are allowed to optimize, store the writers for this sequence step.
	if f.Stats().Optimized() && f.Phase() < OPTIMISEPHASE {
		GetWriterCache().Store(f, f.Stats().CurrentIteration(), writers...)
	}

	atomic.StoreInt64(&f.inited, 1)
	f.iniWriters = append(f.iniWriters, writers...)

	f.begin.Run()
	f.Stats().Next(ms)
	return writers
}

// LastCycles returns the previous cycles count for this sequence frame.
func (f *AnimationSequence) LastCycles() int {
	return int(atomic.LoadInt64(&f.lastCycle))
}

// Cycles return the total completed cycles(forward+reverse) transition for
// this animation sequence.
func (f *AnimationSequence) Cycles() int {
	return int(atomic.LoadInt64(&f.totalCycles))
}

// BeginWriting sets the frame current executing writers as started, which
// allows the frame to ignore any request for next frame when its writers
// have not completed their steps yet.
func (f *AnimationSequence) BeginWriting() {
	atomic.StoreInt64(&f.writesOn, 1)
}

// DoneWriting sets the frame current executing writers as completed, which
// allows the frame issue its next writers sequence.
func (f *AnimationSequence) DoneWriting() {
	atomic.StoreInt64(&f.writesOn, 0)
}

// Continue returns true/false if the frame is ready to issue the next
// sequence writers.
func (f *AnimationSequence) Continue() bool {
	return atomic.LoadInt64(&f.writesOn) == 0
}

// Sync allows the frame to check and perform any update to its operation.
func (f *AnimationSequence) Sync() {
	if f.Stats().IsFirstDone() {
		// Set the completedFrame to one to indicate the frame has completed a full
		// first set animation(transition transition) of its sequences.
		atomic.StoreInt64(&f.completedFrame, 1)
	}

	if f.Stats().IsDone() {
		if f.Stats().Loop() {
			if f.Cycles() < f.Stats().TotalLoops() || f.Stats().TotalLoops() < 0 {

				// Incremement the total cycle count and store the last.
				tc := atomic.LoadInt64(&f.totalCycles)
				{

					// Store the last totalCycles for reference.
					atomic.StoreInt64(&f.lastCycle, tc)

				}
				atomic.AddInt64(&f.totalCycles, 1)

				f.stat = f.stat.Clone()
				return
			}
		}

		f.End()
		f.ended.Run()

		// Iterate the stoppable sequence lists and stop any.
		for _, sq := range f.stoppers {
			sq.Stop()
		}

		// TODO: do we need to flush this? Could the user want to re-use a frame?
		// f.begin.Flush()
		// f.progress.Flush()
		// f.ended.Flush()

		return
	}

	f.progress.Run()
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
func (f *AnimationSequence) Sequence(ms float64) DeferWriters {

	// If we are not allowed to continue then return nil writers
	if !f.Continue() {
		return nil
	}

	var writers DeferWriters

	if f.Stats().Optimized() {

		if f.Phase() > STARTPHASE {
			ct := f.Stats().CurrentIteration()
			writers = GetWriterCache().Writers(f, ct)
			f.Stats().Next(ms)
			return writers
		}
	}

	// Add the BeginWriting writer to setup execution reconciliation policy.
	writers = append(writers, &frameBeginWriter{f: f})

	// Collect all writers from each sequence within the frame.
	for _, seq := range f.sequences {
		writers = append(writers, seq.Next(f.Stats(), f.elementals)...)
	}

	// Add the DoneWriting writer to setup execution reconciliation ending policy.
	writers = append(writers, &frameEndWriter{f: f})

	// If we are allowed to optimize, store the writers for this sequence step.
	if f.Stats().Optimized() && f.Phase() < OPTIMISEPHASE {
		GetWriterCache().Store(f, f.Stats().CurrentIteration(), writers...)
	}

	f.Stats().Next(ms)
	return writers
}
