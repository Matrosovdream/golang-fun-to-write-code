// Package code defines the bytecode: a function is a flat []Instr with a
// constant pool and a parallel table of source positions for error messages.
package code

import (
	"strings"

	"tinylang/internal/token"
)

type Op uint8

// Stack effects are noted as before -> after.
const (
	OpConst     Op = iota // push Consts[A]
	OpNull                // push null
	OpTrue                // push true
	OpFalse               // push false
	OpPop                 // x ->
	OpGet                 // push variable (A = scopes up, B = slot)
	OpSet                 // x -> ; store into variable (A, B)
	OpUndef               // fail: undefined variable Consts[A]
	OpBin                 // l r -> l OP r, OP = token kind A
	OpJump                // ip = A
	OpJumpFalse           // x -> ; ip = A if x is falsy
	OpLoop                // ip = A (backward jump; counts as a step)
	OpCall                // f a1..aA -> result
	OpReturn              // x -> (caller) x
	OpClosure             // push closure of Consts[A] over the current scope
	OpArray               // a1..aA -> array
	OpIndex               // x i -> x[i]
	OpEnter               // open a scope with A slots
	OpLeave               // close it
)

var names = strings.Fields("Const Null True False Pop Get Set Undef Bin Jump JumpFalse Loop Call Return Closure Array Index Enter Leave")

func (o Op) String() string { return names[o] }

type Instr struct {
	Op   Op
	A, B int32
}

type Fn struct {
	NParams, Slots int // Slots: variables in the function's scope, parameters first
	Code           []Instr
	Pos            []token.Pos // Pos[i] is where Code[i] came from
	Consts         []any
}
