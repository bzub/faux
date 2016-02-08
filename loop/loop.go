package loop

//==============================================================================

// Looper defines a loop subscriber for a specific loop subscriber.
type Looper interface {
	End()
}

// GameEngine defines the gameloop interface which defines the cental engine
// behaviour for loop.
type GameEngine interface {
	Loop(Mux) Looper
}

// New returns a new instance object that can use gears to perform its
// run loop.
func New(gears EngineGear) GameEngine {
	em := Engine{gear: gears}
	return &em
}

//==============================================================================

// Mux defines the callback type signature for each runner.
type Mux func(float64)

// EngineGear defines a exposable func type that allows calling a looper internals
// as an instance object.
type EngineGear func(Mux) Looper

//==============================================================================

// Engine defines a internal engine structure
type Engine struct {
	gear EngineGear
}

// Loop calls the engines gear to create the necessary runner.
func (e *Engine) Loop(mx Mux) Looper {
	return e.gear(mx)
}

//==============================================================================
