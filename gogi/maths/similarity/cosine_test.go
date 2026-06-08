package similarity

import (
	"math"
	"testing"
)

func almostEqual(a, b, eps float64) bool {
	return math.Abs(a-b) < eps
}

func TestCosineSimilarity_IdenticalVectors(t *testing.T) {
	a := []float64{1, 2, 3}
	b := []float64{1, 2, 3}

	got := CosineSimilarity(a, b)

	if !almostEqual(got, 1.0, 1e-9) {
		t.Fatalf("expected 1.0, got %f", got)
	}
}

func TestCosineSimilarity_OrthogonalVectors(t *testing.T) {
	a := []float64{1, 0}
	b := []float64{0, 1}

	got := CosineSimilarity(a, b)

	if !almostEqual(got, 0.0, 1e-9) {
		t.Fatalf("expected 0.0, got %f", got)
	}
}

func TestCosineSimilarity_OppositeVectors(t *testing.T) {
	a := []float64{1, 1}
	b := []float64{-1, -1}

	got := CosineSimilarity(a, b)

	if !almostEqual(got, -1.0, 1e-9) {
		t.Fatalf("expected -1.0, got %f", got)
	}
}

func TestCosineSimilarity_DifferentLengths(t *testing.T) {
	a := []float64{1, 2, 3}
	b := []float64{1, 2}

	got := CosineSimilarity(a, b)

	if got != 0 {
		t.Fatalf("expected 0 for different lengths, got %f", got)
	}
}

func TestCosineSimilarity_ZeroVector(t *testing.T) {
	a := []float64{0, 0, 0}
	b := []float64{1, 2, 3}

	got := CosineSimilarity(a, b)

	if got != 0 {
		t.Fatalf("expected 0 for zero vector, got %f", got)
	}
}

func TestCosineSimilarity_PartialSimilarity(t *testing.T) {
	a := []float64{1, 0, 1}
	b := []float64{1, 0, 1}

	got := CosineSimilarity(a, b)

	if !almostEqual(got, 1.0, 1e-9) {
		t.Fatalf("expected 1.0, got %f", got)
	}
}
