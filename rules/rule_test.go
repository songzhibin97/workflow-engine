package rules

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestExprEvaluator tests the ExprEvaluator implementation.
func TestExprEvaluator(t *testing.T) {
	// Initialize the evaluator
	evaluator := NewExprEvaluator()

	// Test cases
	tests := []struct {
		name       string
		expression string
		context    map[string]interface{}
		wantResult bool
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "Valid true expression",
			expression: "age > 18",
			context:    map[string]interface{}{"age": 25},
			wantResult: true,
			wantErr:    false,
		},
		{
			name:       "Valid false expression",
			expression: "age < 18",
			context:    map[string]interface{}{"age": 25},
			wantResult: false,
			wantErr:    false,
		},
		{
			name:       "Non-boolean result",
			expression: "age + 5",
			context:    map[string]interface{}{"age": 25},
			wantResult: false,
			wantErr:    true,
			errMsg:     "expression 'age + 5' did not evaluate to a boolean, got int",
		},
		{
			name:       "Invalid expression",
			expression: "age >>> 18", // Invalid syntax
			context:    map[string]interface{}{"age": 25},
			wantResult: false,
			wantErr:    true,
			errMsg:     "unexpected token", // Updated to match part of the actual error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := evaluator.Evaluate(tt.expression, tt.context)
			if tt.wantErr {
				assert.Error(t, err, "Evaluate() should return an error")
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg, "Error message should match")
				}
				assert.Equal(t, tt.wantResult, result, "Evaluate() result should match even with error")
			} else {
				assert.NoError(t, err, "Evaluate() should not return an error")
				assert.Equal(t, tt.wantResult, result, "Evaluate() result should match")
			}
		})
	}

	// Test caching: Evaluate the same expression twice and ensure consistent results
	t.Run("Caching works", func(t *testing.T) {
		expr := "score > 10"
		context := map[string]interface{}{"score": 15}

		result1, err1 := evaluator.Evaluate(expr, context)
		assert.NoError(t, err1)
		assert.True(t, result1)

		result2, err2 := evaluator.Evaluate(expr, context)
		assert.NoError(t, err2)
		assert.True(t, result2)
	})

	// Test concurrency: Multiple goroutines evaluating expressions
	t.Run("Concurrent evaluation", func(t *testing.T) {
		var wg sync.WaitGroup
		numGoroutines := 100
		expr := "value > 0"
		context := map[string]interface{}{"value": 42}

		wg.Add(numGoroutines)
		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer wg.Done()
				result, err := evaluator.Evaluate(expr, context)
				assert.NoError(t, err)
				assert.True(t, result)
			}()
		}
		wg.Wait()
	})

	// Test concurrency: Multiple goroutines evaluating expressions
	t.Run("Concurrent evaluation", func(t *testing.T) {

		expr := "loop(2) == 'task1_result'"
		context := map[string]interface{}{
			"upstream_results": map[uint64]interface{}{
				3: "task2_result",
				2: "task1_result",
			},
		}
		context["loop"] = func(k int) interface{} {
			if v, ok := context["upstream_results"].(map[uint64]interface{})[uint64(k)]; ok {
				return v
			}
			return nil
		}

		result, err := evaluator.Evaluate(expr, context)
		assert.NoError(t, err)
		assert.True(t, result)
	})
}

// BenchmarkEvaluate benchmarks the performance of Evaluate with and without caching.
func BenchmarkEvaluate(b *testing.B) {
	evaluator := NewExprEvaluator()
	expression := "x > 5"
	context := map[string]interface{}{"x": 10}

	// Reset timer to exclude setup time
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = evaluator.Evaluate(expression, context)
	}
}
