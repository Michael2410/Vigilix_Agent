package collector

import (
	"math"
	"testing"
	"time"
)

func TestDeltaTracker_CalculateNetRates(t *testing.T) {
	dt := NewDeltaTracker()
	t0 := time.Now()

	// 1. Primera lectura: debe retornar ok = false según §5.2
	rates, ok := dt.CalculateNetRates("eth0", 1000, 2000, t0)
	if ok {
		t.Fatalf("esperaba ok=false en la primera lectura, obtuvo ok=true")
	}

	// 2. Segunda lectura a los 2 segundos
	// delta rx = 3000 - 1000 = 2000 bytes en 2s -> 1000 B/s
	// delta tx = 6000 - 2000 = 4000 bytes en 2s -> 2000 B/s
	t1 := t0.Add(2 * time.Second)
	rates, ok = dt.CalculateNetRates("eth0", 3000, 6000, t1)
	if !ok {
		t.Fatalf("esperaba ok=true en la segunda lectura")
	}

	if rates.RxBps != 1000.0 {
		t.Errorf("esperaba RxBps = 1000.0, obtuvo %f", rates.RxBps)
	}
	if rates.TxBps != 2000.0 {
		t.Errorf("esperaba TxBps = 2000.0, obtuvo %f", rates.TxBps)
	}

	// 3. Manejo de reinicio o caída de contador (overflow)
	t2 := t1.Add(1 * time.Second)
	rates, ok = dt.CalculateNetRates("eth0", 500, 1000, t2)
	if ok {
		t.Fatalf("esperaba ok=false ante reset de contador, obtuvo ok=true")
	}
}

func TestDeltaTracker_CalculateCPURate(t *testing.T) {
	dt := NewDeltaTracker()
	t0 := time.Now()

	// Primera lectura
	_, ok := dt.CalculateCPURate(10000, 8000, t0)
	if ok {
		t.Fatalf("esperaba ok=false en la primera lectura")
	}

	// Segunda lectura: 1000 ticks totales, 700 ticks idle -> 300 ticks usados = 30%
	t1 := t0.Add(time.Second)
	pct, ok := dt.CalculateCPURate(11000, 8700, t1)
	if !ok {
		t.Fatalf("esperaba ok=true en la segunda lectura")
	}
	if math.Abs(pct-30.0) > 0.1 {
		t.Errorf("esperaba CPU pct ~30.0, obtuvo %f", pct)
	}
}
