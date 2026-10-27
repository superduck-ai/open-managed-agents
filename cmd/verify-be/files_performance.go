package main

import (
	"errors"
	"math"
	"slices"
)

const filesPerformanceWorkload = "files.serial.v1:warmup=2:samples=20:bytes=32768:concurrency=1:gap_ms=250"

type filesLatencySample struct {
	Index    int     `json:"index"`
	Upload   float64 `json:"upload_ms"`
	Metadata float64 `json:"metadata_ms"`
	List     float64 `json:"list_ms"`
	Download float64 `json:"download_ms"`
	Delete   float64 `json:"delete_ms"`
}

func workloadName(selected string) string {
	if selected == "files.performance" {
		return filesPerformanceWorkload
	}
	return performanceWorkload
}

func summarizePerformance(r report) (map[string]latencyStats, error) {
	if r.Scenario == "chat.performance" {
		return summarizeSamples(r.Result.Samples)
	}
	if r.Scenario != "files.performance" || len(r.Result.FilesSamples) != performanceSamples || len(r.Result.Samples) != 0 {
		return nil, errors.New("Files performance requires exactly twenty Files samples")
	}
	values := map[string][]float64{"upload": {}, "metadata": {}, "list": {}, "download": {}, "delete": {}}
	for i, sample := range r.Result.FilesSamples {
		if sample.Index != i {
			return nil, errors.New("invalid Files performance sample order")
		}
		for name, value := range map[string]float64{"upload": sample.Upload, "metadata": sample.Metadata, "list": sample.List, "download": sample.Download, "delete": sample.Delete} {
			if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, errors.New("invalid Files performance duration")
			}
			values[name] = append(values[name], value)
		}
	}
	return latencySummary(values), nil
}

func latencySummary(values map[string][]float64) map[string]latencyStats {
	result := map[string]latencyStats{}
	for name, measurements := range values {
		slices.Sort(measurements)
		result[name] = latencyStats{P50: measurements[(len(measurements)-1)/2], P95: measurements[int(math.Ceil(float64(len(measurements))*.95))-1]}
	}
	return result
}
