//go:build research

package ml

import (
	"math"
	"testing"
)

// === VALUE ARITHMETIC TESTS ===

func TestValueAdd(t *testing.T) {
	a := V(2.0)
	b := V(3.0)
	c := a.Add(b)

	if c.Data != 5.0 {
		t.Errorf("got %f, want 5.0", c.Data)
	}

	c.Backward()
	if a.Grad != 1.0 {
		t.Errorf("da = %f, want 1.0", a.Grad)
	}
	if b.Grad != 1.0 {
		t.Errorf("db = %f, want 1.0", b.Grad)
	}
}

func TestValueMul(t *testing.T) {
	a := V(3.0)
	b := V(4.0)
	c := a.Mul(b)

	if c.Data != 12.0 {
		t.Errorf("got %f, want 12.0", c.Data)
	}

	c.Backward()
	// d(a*b)/da = b = 4, d(a*b)/db = a = 3
	if a.Grad != 4.0 {
		t.Errorf("da = %f, want 4.0", a.Grad)
	}
	if b.Grad != 3.0 {
		t.Errorf("db = %f, want 3.0", b.Grad)
	}
}

func TestValuePow(t *testing.T) {
	a := V(3.0)
	c := a.Pow(2) // 9

	c.Backward()
	// d(x^2)/dx = 2x = 6
	if math.Abs(a.Grad-6.0) > 1e-10 {
		t.Errorf("da = %f, want 6.0", a.Grad)
	}
}

func TestValueDiv(t *testing.T) {
	a := V(6.0)
	b := V(3.0)
	c := a.Div(b) // 2.0

	if math.Abs(c.Data-2.0) > 1e-10 {
		t.Errorf("got %f, want 2.0", c.Data)
	}
}

func TestValueSub(t *testing.T) {
	a := V(5.0)
	b := V(3.0)
	c := a.Sub(b)

	if c.Data != 2.0 {
		t.Errorf("got %f, want 2.0", c.Data)
	}
}

func TestValueNeg(t *testing.T) {
	a := V(5.0)
	c := a.Neg()
	if c.Data != -5.0 {
		t.Errorf("got %f, want -5.0", c.Data)
	}
}

// === ACTIVATION TESTS ===

func TestValueReLU(t *testing.T) {
	tests := []struct {
		in, wantOut, wantGrad float64
	}{
		{3.0, 3.0, 1.0},
		{-2.0, 0.0, 0.0},
		{0.0, 0.0, 0.0},
	}
	for _, tt := range tests {
		a := V(tt.in)
		c := a.ReLU()
		if c.Data != tt.wantOut {
			t.Errorf("relu(%f) = %f, want %f", tt.in, c.Data, tt.wantOut)
		}
		c.Backward()
		if a.Grad != tt.wantGrad {
			t.Errorf("relu'(%f) = %f, want %f", tt.in, a.Grad, tt.wantGrad)
		}
	}
}

func TestValueTanh(t *testing.T) {
	a := V(0.5)
	c := a.Tanh()
	want := math.Tanh(0.5)
	if math.Abs(c.Data-want) > 1e-10 {
		t.Errorf("tanh(0.5) = %f, want %f", c.Data, want)
	}

	c.Backward()
	// d(tanh(x))/dx = 1 - tanh(x)^2
	wantGrad := 1 - want*want
	if math.Abs(a.Grad-wantGrad) > 1e-10 {
		t.Errorf("tanh'(0.5) = %f, want %f", a.Grad, wantGrad)
	}
}

func TestValueExp(t *testing.T) {
	a := V(1.0)
	c := a.Exp()
	if math.Abs(c.Data-math.E) > 1e-10 {
		t.Errorf("exp(1) = %f, want %f", c.Data, math.E)
	}

	c.Backward()
	// d(e^x)/dx = e^x
	if math.Abs(a.Grad-math.E) > 1e-10 {
		t.Errorf("exp'(1) = %f, want %f", a.Grad, math.E)
	}
}

func TestValueLog(t *testing.T) {
	a := V(math.E)
	c := a.Log()
	if math.Abs(c.Data-1.0) > 1e-10 {
		t.Errorf("log(e) = %f, want 1.0", c.Data)
	}

	c.Backward()
	// d(log(x))/dx = 1/x
	if math.Abs(a.Grad-1.0/math.E) > 1e-10 {
		t.Errorf("log'(e) = %f, want %f", a.Grad, 1.0/math.E)
	}
}

func TestValueSafeLog(t *testing.T) {
	// Normal case
	a := V(0.5)
	c := a.SafeLog()
	if math.Abs(c.Data-math.Log(0.5)) > 1e-10 {
		t.Errorf("safelog(0.5) = %f, want %f", c.Data, math.Log(0.5))
	}

	// Near-zero case: should clamp to log(1e-10) instead of -inf
	b := V(0.0)
	d := b.SafeLog()
	if math.IsInf(d.Data, -1) {
		t.Error("safelog(0) should not be -inf")
	}
	if math.Abs(d.Data-math.Log(1e-10)) > 1e-10 {
		t.Errorf("safelog(0) = %f, want %f", d.Data, math.Log(1e-10))
	}
}

// === CHAIN RULE / COMPOSITION TESTS ===

func TestValueChainRule(t *testing.T) {
	// f(x) = (x + 1) * (x + 2)
	// f'(x) = 2x + 3
	// At x=3: f(3) = 4*5 = 20, f'(3) = 9
	x := V(3.0)
	a := x.AddScalar(1) // x + 1
	b := x.AddScalar(2) // x + 2
	c := a.Mul(b)       // (x+1)*(x+2)

	if c.Data != 20.0 {
		t.Errorf("f(3) = %f, want 20.0", c.Data)
	}

	c.Backward()
	// x contributes to both a and b, so grad accumulates:
	// dc/dx = dc/da * da/dx + dc/db * db/dx = b.Data * 1 + a.Data * 1 = 5 + 4 = 9
	if math.Abs(x.Grad-9.0) > 1e-10 {
		t.Errorf("f'(3) = %f, want 9.0", x.Grad)
	}
}

func TestValueComplexExpression(t *testing.T) {
	// f(a,b) = tanh(a*b + a)
	// Numerical gradient check
	a := V(0.5)
	b := V(0.7)
	c := a.Mul(b).Add(a).Tanh()

	c.Backward()

	// Numerical gradient for a
	eps := 1e-5
	a1 := 0.5 + eps
	a2 := 0.5 - eps
	f1 := math.Tanh(a1*0.7 + a1)
	f2 := math.Tanh(a2*0.7 + a2)
	numGradA := (f1 - f2) / (2 * eps)

	if math.Abs(a.Grad-numGradA) > 1e-4 {
		t.Errorf("da: analytical=%f, numerical=%f", a.Grad, numGradA)
	}

	// Numerical gradient for b
	b1 := 0.7 + eps
	b2 := 0.7 - eps
	f1 = math.Tanh(0.5*b1 + 0.5)
	f2 = math.Tanh(0.5*b2 + 0.5)
	numGradB := (f1 - f2) / (2 * eps)

	if math.Abs(b.Grad-numGradB) > 1e-4 {
		t.Errorf("db: analytical=%f, numerical=%f", b.Grad, numGradB)
	}
}

// === UTILITY FUNCTION TESTS ===

func TestVSoftmax(t *testing.T) {
	logits := []*Value{V(1), V(2), V(3)}
	probs := VSoftmax(logits)

	// Probabilities should sum to 1
	var sum float64
	for _, p := range probs {
		sum += p.Data
	}
	if math.Abs(sum-1.0) > 1e-10 {
		t.Errorf("softmax sum = %f, want 1.0", sum)
	}

	// Monotonicity
	if probs[0].Data >= probs[1].Data || probs[1].Data >= probs[2].Data {
		t.Errorf("softmax not monotonic: %v", []float64{probs[0].Data, probs[1].Data, probs[2].Data})
	}
}

func TestVSoftmaxGradient(t *testing.T) {
	// Check that gradients flow through softmax
	logits := []*Value{V(1), V(2), V(3)}
	probs := VSoftmax(logits)

	// Loss = -log(probs[2]) (cross-entropy targeting index 2)
	loss := probs[2].SafeLog().Neg()
	loss.Backward()

	// Gradient of true class logit should be negative (increasing it reduces loss)
	if logits[2].Grad >= 0 {
		t.Errorf("gradient for correct class should be negative, got %f", logits[2].Grad)
	}
}

func TestVLinear(t *testing.T) {
	// y = W @ x, W = [[1,2],[3,4]], x = [5,6]
	// y = [1*5+2*6, 3*5+4*6] = [17, 39]
	w := [][]*Value{{V(1), V(2)}, {V(3), V(4)}}
	x := []*Value{V(5), V(6)}
	y := VLinear(x, w)

	if math.Abs(y[0].Data-17.0) > 1e-10 {
		t.Errorf("y[0] = %f, want 17.0", y[0].Data)
	}
	if math.Abs(y[1].Data-39.0) > 1e-10 {
		t.Errorf("y[1] = %f, want 39.0", y[1].Data)
	}
}

func TestVRMSNorm(t *testing.T) {
	x := []*Value{V(1), V(2), V(3)}
	normed := VRMSNorm(x)

	// After RMSNorm, rms(output) should be ~1.0
	var sumSq float64
	for _, v := range normed {
		sumSq += v.Data * v.Data
	}
	rms := math.Sqrt(sumSq / float64(len(normed)))
	if math.Abs(rms-1.0) > 1e-3 {
		t.Errorf("rms after normalization = %f, want ~1.0", rms)
	}

	// Proportions should be preserved
	ratio := normed[1].Data / normed[0].Data
	if math.Abs(ratio-2.0) > 1e-3 {
		t.Errorf("ratio normed[1]/normed[0] = %f, want 2.0", ratio)
	}
}

// === BENCHMARKS ===

func BenchmarkValueForwardBackward(b *testing.B) {
	// Simulate a small neural network forward+backward pass
	for b.Loop() {
		x := V(0.5)
		// Two-layer: relu(W2 @ relu(W1 @ x))
		h1 := x.MulScalar(0.3).AddScalar(0.1).ReLU()
		h2 := h1.MulScalar(-0.5).AddScalar(0.2).ReLU()
		loss := h2.Pow(2) // MSE-like
		loss.Backward()
	}
}

func BenchmarkVSoftmax(b *testing.B) {
	logits := make([]*Value, 27) // typical char vocab
	for i := range logits {
		logits[i] = V(float64(i) * 0.1)
	}
	b.ResetTimer()
	for b.Loop() {
		VSoftmax(logits)
	}
}
