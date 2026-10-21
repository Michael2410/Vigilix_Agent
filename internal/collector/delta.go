package collector

import "time"

type NetCounterSample struct {
	BytesRecv uint64
	BytesSent uint64
	At        time.Time
}

type CPUTickSample struct {
	Total   uint64
	IdleAll uint64
	At      time.Time
}

type DeltaTracker struct {
	lastNetCounters map[string]NetCounterSample
	lastCPUTicks    *CPUTickSample
}

func NewDeltaTracker() *DeltaTracker {
	return &DeltaTracker{
		lastNetCounters: make(map[string]NetCounterSample),
	}
}

type NetRates struct {
	RxBps float64
	TxBps float64
}

// CalculateNetRates calcula bytes/segundo para una interfaz.
// Si es la primera lectura para la interfaz, devuelve ok = false (no se envía métrica según §5.2).
func (dt *DeltaTracker) CalculateNetRates(iface string, bytesRecv, bytesSent uint64, at time.Time) (NetRates, bool) {
	prev, exists := dt.lastNetCounters[iface]
	dt.lastNetCounters[iface] = NetCounterSample{
		BytesRecv: bytesRecv,
		BytesSent: bytesSent,
		At:        at,
	}

	if !exists {
		return NetRates{}, false
	}

	elapsed := at.Sub(prev.At).Seconds()
	if elapsed <= 0 {
		return NetRates{}, false
	}

	// Manejo de desbordamiento o reinicio de interfaz
	if bytesRecv < prev.BytesRecv || bytesSent < prev.BytesSent {
		return NetRates{}, false
	}

	rxDelta := float64(bytesRecv - prev.BytesRecv)
	txDelta := float64(bytesSent - prev.BytesSent)

	return NetRates{
		RxBps: rxDelta / elapsed,
		TxBps: txDelta / elapsed,
	}, true
}

// CalculateCPURate calcula el porcentaje de uso de CPU a partir de ticks acumulativos de /proc/stat.
// Si es la primera lectura, devuelve ok = false.
func (dt *DeltaTracker) CalculateCPURate(total, idleAll uint64, at time.Time) (float64, bool) {
	prev := dt.lastCPUTicks
	dt.lastCPUTicks = &CPUTickSample{
		Total:   total,
		IdleAll: idleAll,
		At:      at,
	}

	if prev == nil {
		return 0, false
	}

	if total <= prev.Total {
		return 0, false
	}

	deltaTotal := float64(total - prev.Total)
	deltaIdle := float64(idleAll - prev.IdleAll)

	if deltaTotal <= 0 {
		return 0, false
	}

	pct := (1.0 - (deltaIdle / deltaTotal)) * 100.0
	if pct < 0 {
		pct = 0
	} else if pct > 100 {
		pct = 100
	}

	return pct, true
}
