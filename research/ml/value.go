//go:build research

// Scalar autograd engine: reverse-mode automatic differentiation over float64 scalars.
//
// Tracks computational history via children and local gradients, enabling gradient
// computation through the chain rule. Every forward operation stores its local
// derivative (dout/dinput), then Backward replays the computation graph in reverse
// topological order, accumulating gradients.
//
// This Value type follows the canonical interface from mathews-tom/no-magic and is
// used by microgpt, micrornn, and other training-capable algorithm ports.
//
// Port of the Value class from mathews-tom/no-magic to Go.
// AGPL-3.0 -- see LICENSE in repo root.
package ml

import "math"

// Value is a scalar with reverse-mode automatic differentiation.
//
// Every arithmetic operation creates a new Value that remembers its parent
// Values (children) and the local gradient of the operation (dself/dchild).
// Calling Backward on a loss Value propagates gradients through the entire
// computation graph via the chain rule.
type Value struct {
	Data       float64
	Grad       float64
	children   []*Value
	localGrads []float64
}

// V creates a leaf Value (no parents in the computation graph).
func V(data float64) *Value {
	return &Value{Data: data}
}

// newValue creates a Value with children and local gradients.
func newValue(data float64, children []*Value, localGrads []float64) *Value {
	return &Value{
		Data:       data,
		children:   children,
		localGrads: localGrads,
	}
}

// === ARITHMETIC OPERATIONS ===

// Add returns a + b.  d(a+b)/da = 1, d(a+b)/db = 1.
func (a *Value) Add(b *Value) *Value {
	return newValue(a.Data+b.Data, []*Value{a, b}, []float64{1, 1})
}

// AddScalar returns a + s.
func (a *Value) AddScalar(s float64) *Value {
	return a.Add(V(s))
}

// Mul returns a * b.  d(a*b)/da = b, d(a*b)/db = a.
func (a *Value) Mul(b *Value) *Value {
	return newValue(a.Data*b.Data, []*Value{a, b}, []float64{b.Data, a.Data})
}

// MulScalar returns a * s.
func (a *Value) MulScalar(s float64) *Value {
	return a.Mul(V(s))
}

// Pow returns a^n.  d(x^n)/dx = n * x^(n-1).
func (a *Value) Pow(n float64) *Value {
	return newValue(math.Pow(a.Data, n), []*Value{a}, []float64{n * math.Pow(a.Data, n-1)})
}

// Neg returns -a.
func (a *Value) Neg() *Value {
	return a.MulScalar(-1)
}

// Sub returns a - b.
func (a *Value) Sub(b *Value) *Value {
	return a.Add(b.Neg())
}

// Div returns a / b.
func (a *Value) Div(b *Value) *Value {
	return a.Mul(b.Pow(-1))
}

// DivScalar returns a / s.
func (a *Value) DivScalar(s float64) *Value {
	return a.MulScalar(1.0 / s)
}

// === ACTIVATION FUNCTIONS ===

// Tanh returns tanh(a).  d(tanh(x))/dx = 1 - tanh(x)^2.
func (a *Value) Tanh() *Value {
	t := math.Tanh(a.Data)
	return newValue(t, []*Value{a}, []float64{1 - t*t})
}

// Exp returns e^a.  d(e^x)/dx = e^x.
func (a *Value) Exp() *Value {
	e := math.Exp(a.Data)
	return newValue(e, []*Value{a}, []float64{e})
}

// Log returns log(a).  d(log(x))/dx = 1/x.
// Caller must ensure a.Data > 0.
func (a *Value) Log() *Value {
	return newValue(math.Log(a.Data), []*Value{a}, []float64{1.0 / a.Data})
}

// SafeLog returns log(max(a, 1e-10)) while preserving the gradient path through a.
// Prevents log(0) = -inf which breaks backpropagation.
func (a *Value) SafeLog() *Value {
	clamped := math.Max(a.Data, 1e-10)
	return newValue(math.Log(clamped), []*Value{a}, []float64{1.0 / clamped})
}

// ReLU returns max(0, a).  d(relu(x))/dx = 1 if x > 0, else 0.
func (a *Value) ReLU() *Value {
	var out, grad float64
	if a.Data > 0 {
		out = a.Data
		grad = 1.0
	}
	return newValue(out, []*Value{a}, []float64{grad})
}

// === BACKPROPAGATION ===

// Backward computes gradients via reverse-mode automatic differentiation.
//
// Builds a topological ordering of the computation graph, then propagates
// gradients backward using the chain rule. For a composite function
// f(g(h(x))), the chain rule says df/dx = (df/dg) * (dg/dh) * (dh/dx).
// The topological sort ensures we compute df/dg before we need it for df/dh.
func (v *Value) Backward() {
	// Topological sort
	var topo []*Value
	visited := make(map[*Value]bool)

	var buildTopo func(*Value)
	buildTopo = func(node *Value) {
		if visited[node] {
			return
		}
		visited[node] = true
		for _, child := range node.children {
			buildTopo(child)
		}
		topo = append(topo, node)
	}
	buildTopo(v)

	// Seed: gradient of loss with respect to itself is 1
	v.Grad = 1.0

	// Reverse topological order: gradients flow backward from output to inputs
	for i := len(topo) - 1; i >= 0; i-- {
		node := topo[i]
		for j, child := range node.children {
			// Chain rule: dLoss/dchild += dLoss/dnode * dnode/dchild
			child.Grad += node.localGrads[j] * node.Grad
		}
	}
}

// ZeroGrad resets the gradient to 0. Call before each backward pass when
// reusing parameters across training steps.
func (v *Value) ZeroGrad() {
	v.Grad = 0.0
}

// === UTILITY FUNCTIONS FOR Value SLICES ===

// VSoftmax computes numerically stable softmax over a slice of Values.
//
// softmax(x_i) = exp(x_i - max(x)) / sum_j(exp(x_j - max(x)))
// Subtracting max prevents exp() overflow.
func VSoftmax(logits []*Value) []*Value {
	maxVal := logits[0].Data
	for _, v := range logits[1:] {
		if v.Data > maxVal {
			maxVal = v.Data
		}
	}
	expVals := make([]*Value, len(logits))
	total := V(0)
	for i, v := range logits {
		expVals[i] = v.AddScalar(-maxVal).Exp()
		total = total.Add(expVals[i])
	}
	result := make([]*Value, len(logits))
	for i, e := range expVals {
		result[i] = e.Div(total)
	}
	return result
}

// VLinear computes y = W @ x (no bias) where W is [nOut][nIn] and x is [nIn].
func VLinear(x []*Value, w [][]*Value) []*Value {
	out := make([]*Value, len(w))
	for i, wRow := range w {
		sum := V(0)
		for j := range x {
			sum = sum.Add(wRow[j].Mul(x[j]))
		}
		out[i] = sum
	}
	return out
}

// VRMSNorm applies Root Mean Square normalization to a vector of Values.
//
// RMSNorm(x) = x / sqrt(mean(x^2) + eps)
// Simpler than LayerNorm (no mean centering or learned parameters), used in
// LLaMA, Gemma, and other modern architectures.
func VRMSNorm(x []*Value) []*Value {
	n := float64(len(x))
	meanSq := V(0)
	for _, xi := range x {
		meanSq = meanSq.Add(xi.Mul(xi))
	}
	meanSq = meanSq.DivScalar(n)
	scale := meanSq.AddScalar(1e-5).Pow(-0.5)
	out := make([]*Value, len(x))
	for i, xi := range x {
		out[i] = xi.Mul(scale)
	}
	return out
}

// VSum returns the sum of all Values.
func VSum(vals []*Value) *Value {
	sum := V(0)
	for _, v := range vals {
		sum = sum.Add(v)
	}
	return sum
}

// MakeVMatrix creates a weight matrix of Values initialized with Gaussian noise.
func MakeVMatrix(rng interface{ NormFloat64() float64 }, nRows, nCols int, std float64) [][]*Value {
	m := make([][]*Value, nRows)
	for i := range m {
		m[i] = make([]*Value, nCols)
		for j := range m[i] {
			m[i][j] = V(rng.NormFloat64() * std)
		}
	}
	return m
}
