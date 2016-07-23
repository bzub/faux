// Package pub provides a functional reactive pubsub structure to leverage a
// pure function style reactive behaviour. Originally pulled from pub.Node.
package pub

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/influx6/faux/context"
	"github.com/influx6/faux/reflection"
	"github.com/satori/go.uuid"
)

var (
	errorType = reflect.TypeOf((*error)(nil)).Elem()
	ctxType   = reflect.TypeOf((*Ctx)(nil)).Elem()
)

// ReadWriter defines a type which defines a Reader and Writer interface conforming
// methods.
type ReadWriter interface {
	Reader
	Writer
}

// Node provides an interface definition for the Node type, to allow
// compatibility by future extenders when composing with other structs.
type Node interface {
	ReadWriter
	Reactor

	UUID() string
}

// Ctx defines a type which is passed into all Handlers to provide access
// to an underline context.Context provider and the source Read and Write methods.
type Ctx interface {
	Ctx() context.Context
	RW() ReadWriter
}

// Handler defines a function type which processes data and accepts a ReadWriter
// through which it sends its reply.
type Handler func(Ctx, error, interface{})

// Sync returns a new functional Node.
func Sync(op Handler) Node {
	node := pub{
		op:   op,
		uuid: uuid.NewV4().String(),
	}

	return &node
}

// ASync returns a new functional Node.
func ASync(op Handler) Node {
	node := pub{
		op:    op,
		async: true,
		uuid:  uuid.NewV4().String(),
	}

	return &node
}

//==============================================================================

// DataHandler defines a function type that concentrates on handling only data
// replies alone.
type DataHandler func(Ctx, interface{})

// WrapData returns a Handler which wraps a DataHandler within it, but
// passing forward all errors it receives.
func WrapData(dh DataHandler) Handler {
	return func(m Ctx, err error, data interface{}) {
		if err != nil {
			m.RW().Write(m, err)
			return
		}
		dh(m, data)
	}
}

// ErrorHandler defines a function type that concentrates on handling only data
// errors alone.
type ErrorHandler func(Ctx, error)

// WrapError returns a Handler which wraps a DataHandler within it, but
// passing forward all errors it receives.
func WrapError(dh ErrorHandler) Handler {
	return func(m Ctx, err error, data interface{}) {
		if err == nil {
			dh(m, err)
			return
		}
		m.RW().Write(m, data)
	}
}

//==============================================================================

// DSync returns a new functional Node using the DataHandler.
func DSync(dh DataHandler) Node {
	node := pub{
		op:   WrapData(dh),
		uuid: uuid.NewV4().String(),
	}

	return &node
}

// DASync returns a new functional Node using the DataHandler.
func DASync(dh DataHandler) Node {
	node := pub{
		async: true,
		op:    WrapData(dh),
		uuid:  uuid.NewV4().String(),
	}

	return &node
}

//==============================================================================

// pub provides a pure functional Node, which uses an internal wait group to
// ensure if close is called that call values where delivered.
type pub struct {
	uuid string
	op   Handler
	root Node

	async bool
	rw    sync.RWMutex
	subs  []Node
}

// UUID returns the Node unique identification.
func (p *pub) UUID() string {
	return p.uuid
}

// Reader defines the delivery methods used to deliver data into Node process.
type Reader interface {
	Read(v interface{}, ctx ...context.Context)
}

// context defines a struct which composes both a context.Ctx and a
type contxt struct {
	ctx context.Context
	rw  ReadWriter
}

// Ctx returns the context.Context for this struct.
func (c contxt) Ctx() context.Context {
	return c.ctx
}

// RW returns the ReadWriter for this struct.
func (c contxt) RW() ReadWriter {
	return c.rw
}

// Send applies a message value to the handler.
func (p *pub) Read(b interface{}, ctxs ...context.Context) {
	var ctx context.Context

	if len(ctxs) < 1 {
		ctx = context.New()
	} else {
		ctx = ctxs[0]
	}

	ctxn := &contxt{
		ctx: ctx,
		rw:  p,
	}

	if err, ok := b.(error); ok {
		if p.async {
			go p.op(ctxn, err, nil)
			return
		}

		p.op(ctxn, err, nil)
		return
	}

	if p.async {
		go p.op(ctxn, nil, b)
		return
	}

	p.op(ctxn, nil, b)
}

// NthFinder defines a function type which takes the length and index to
// return a new index value.
type NthFinder func(index int, length int) (NewIndex int)

// Writer defines reply methods to reply to requests
type Writer interface {
	Write(Ctx, interface{})
	WriteEvery(Ctx, interface{}, NthFinder)
}

// Write allows the reply of an data message.
// Note: We use the variadic format for the context but only one is used.
func (p *pub) Write(ctx Ctx, v interface{}) {
	ctxn := &contxt{
		ctx: ctx.Ctx(),
		rw:  p,
	}

	var isErr bool

	// Grab the error if it indeed is an error once.
	err, ok := v.(error)
	if ok {
		isErr = true
	}

	p.rw.RLock()
	{
		for _, node := range p.subs {
			if isErr {
				node.Write(ctxn, err)
				continue
			}
			node.Write(ctxn, v)
		}
	}
	p.rw.RUnlock()

}

func defaultFinder(index int, length int) int {
	return index
}

// WriteEvery allows the delivery/publish of a response to selected index of
// registered nodes using the finder function provided else delivers to all nodes.
// Note: We use the variadic format for the context but only one is used.
func (p *pub) WriteEvery(ctx Ctx, v interface{}, finder NthFinder) {
	ctxn := &contxt{
		ctx: ctx.Ctx(),
		rw:  p,
	}

	if finder == nil {
		finder = defaultFinder
	}

	var isErr bool

	// Grab the error if it indeed is an error once.
	err, ok := v.(error)
	if ok {
		isErr = true
	}

	nlen := len(p.subs)

	p.rw.RLock()
	{
		for index := 0; index < nlen; index++ {
			newIndex := finder(index, nlen)

			if newIndex > 0 && newIndex < nlen {
				node := p.subs[index]

				if isErr {
					node.Write(ctxn, err)
					continue
				}
				node.Write(ctxn, v)
			}

		}
	}
	p.rw.RUnlock()

}

// Reactor defines the core connecting methods used for binding with a Node.
type Reactor interface {
	Signal(interface{}) Node
	AsyncSignal(interface{}) Node

	SignalD(DataHandler) Node
	AsyncSignalD(DataHandler) Node

	SignalE(ErrorHandler) Node
	AsyncSignalE(ErrorHandler) Node
}

// SignalD binds the provided Handler to recieve data feeds only synchronously.
func (p *pub) SignalD(dh DataHandler) Node {
	return p.Signal(WrapData(dh))
}

// AsyncSignalD binds the provided Handler to recieve data feeds only asynchronously.
func (p *pub) AsyncSignalD(dh DataHandler) Node {
	return p.AsyncSignal(WrapData(dh))
}

// SignalE binds the provided Handler to recieve error feeds only synchronously.
func (p *pub) SignalE(dh ErrorHandler) Node {
	return p.Signal(WrapError(dh))
}

// AsyncSignalE binds the provided Handler to recieve error feeds only asynchronously.
func (p *pub) AsyncSignalE(dh ErrorHandler) Node {
	return p.AsyncSignal(WrapError(dh))
}

// AsyncSignal sends the response signal from this Node to the provided node
// within a goroutine. If the input is a Node then it is returned.
func (p *pub) AsyncSignal(node interface{}) Node {
	var n Node

	switch node.(type) {
	case Node:
		n = node.(Node)
	case Handler:
		n = ASync(node.(Handler))
	case ErrorHandler:
		dh := node.(ErrorHandler)
		n = ASync(func(m Ctx, err error, val interface{}) {
			if err == nil {
				dh(m, err)
				return
			}

			m.RW().Write(m, val)
		})
	case DataHandler:
		dh := node.(DataHandler)
		n = ASync(func(m Ctx, err error, val interface{}) {
			if err != nil {
				m.RW().Write(m, err)
				return
			}
			dh(m, val)
		})
	default:
		fmt.Printf("Calling sync default\n")
		if !reflection.IsFuncType(node) {
			return nil
		}

		tm, _ := reflection.FuncValue(node)
		args, _ := reflection.GetFuncArgumentsType(node)

		if alen := len(args); alen < 3 || alen > 3 {
			return nil
		}

		// Check if this first item is a pub.Ctx type.
		if ok, _ := reflection.CanSetForType(ctxType, args[0]); !ok {
			return nil
		}

		// Check if this second item is a error type.
		if ok, _ := reflection.CanSetForType(errorType, args[1]); !ok {
			return nil
		}

		data := args[2]
		n = ASync(func(m Ctx, err error, val interface{}) {
			ma := reflect.ValueOf(m)
			erm := reflect.ValueOf(err)

			if err != nil {
				tm.Call([]reflect.Value{ma, erm, reflect.ValueOf(nil)})
				return
			}

			mVal := reflect.ValueOf(val)

			ok, convert := reflection.CanSetFor(data, mVal)
			if !ok {
				return
			}

			var relVal reflect.Value
			if convert {
				relVal, err = reflection.Convert(data, mVal)
				if err != nil {
					return
				}
			}

			tm.Call([]reflect.Value{ma, erm, relVal})
		})

	}

	p.rw.Lock()
	{
		p.subs = append(p.subs, n)
	}
	p.rw.Unlock()

	return n
}

// Signal sends the response signal from this Node to the provided node.
// If the input is a Node then it is returned, if its a Handler or DataHandler
// then a new Node instance is returned.
func (p *pub) Signal(node interface{}) Node {
	var n Node

	switch node.(type) {
	case Node:
		n = node.(Node)
	case Handler:
		n = Sync(node.(Handler))
	case ErrorHandler:
		dh := node.(ErrorHandler)
		n = Sync(func(m Ctx, err error, val interface{}) {
			if err == nil {
				dh(m, err)
				return
			}

			m.RW().Write(m, val)
		})
	case DataHandler:
		dh := node.(DataHandler)
		n = Sync(func(m Ctx, err error, val interface{}) {
			if err != nil {
				m.RW().Write(m, err)
				return
			}
			dh(m, val)
		})
	default:
		fmt.Printf("Calling sync default\n")
		if !reflection.IsFuncType(node) {
			return nil
		}

		tm, _ := reflection.FuncValue(node)
		args, _ := reflection.GetFuncArgumentsType(node)

		if alen := len(args); alen < 3 || alen > 3 {
			return nil
		}

		// Check if this first item is a pub.Ctx type.
		if ok, _ := reflection.CanSetForType(ctxType, args[0]); !ok {
			return nil
		}

		// Check if this second item is a error type.
		if ok, _ := reflection.CanSetForType(errorType, args[1]); !ok {
			return nil
		}

		data := args[2]
		n = Sync(func(m Ctx, err error, val interface{}) {
			ma := reflect.ValueOf(m)
			erm := reflect.ValueOf(err)

			if err != nil {
				tm.Call([]reflect.Value{ma, erm, reflect.ValueOf(nil)})
				return
			}

			mVal := reflect.ValueOf(val)

			ok, convert := reflection.CanSetFor(data, mVal)
			if !ok {
				return
			}

			var relVal reflect.Value
			if convert {
				relVal, err = reflection.Convert(data, mVal)
				if err != nil {
					return
				}
			}

			tm.Call([]reflect.Value{ma, erm, relVal})
		})
	}

	p.rw.Lock()
	{
		p.subs = append(p.subs, n)
	}
	p.rw.Unlock()

	return n
}

//==============================================================================

// Lift runs through the giving list of ReadWriters and connects them serialy.
// Chain the next to the previous node.
func Lift(rws ...Reactor) {
	rwsLen := len(rws)

	for index := 0; index < rwsLen; index++ {
		if index < 1 {
			continue
		}

		node := rws[index]
		pnode := rws[index-1]
		pnode.Signal(node)
	}
}

// DeLift runs through the giving list of ReadWriters and connects them
// inversely serialy, chaining the nodes in the inverse order.
func DeLift(rws ...Reactor) {
	rwsLen := len(rws)

	for index := rwsLen - 1; index >= 0; index-- {
		if index >= rwsLen-1 {
			continue
		}

		pnode := rws[index]
		node := rws[index-1]

		pnode.Signal(node)
	}
}
