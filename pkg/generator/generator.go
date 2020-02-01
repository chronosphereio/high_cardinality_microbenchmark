package generator

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/influxdata/influxdb-comparisons/bulk_data_gen/common"
	"github.com/influxdata/influxdb-comparisons/bulk_data_gen/devops"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/prompb"
)

type HostsSimulator struct {
	sync.RWMutex
	hosts     []devops.Host
	allHosts  []devops.Host
	hostIndex int
	timeNowFn func() time.Time
}

type HostsSimulatorOptions struct {
	Labels    map[string]string
	TimeNowFn func() time.Time
}

func NewHostsSimulator(
	hostCount int,
	start time.Time,
	opts HostsSimulatorOptions,
) *HostsSimulator {
	var hosts []devops.Host
	for i := 0; i < hostCount; i++ {
		host := devops.NewHost(i, 0, start)
		hosts = append(hosts, host)
	}

	timeNowFn := time.Now
	if opts.TimeNowFn != nil {
		timeNowFn = opts.TimeNowFn
	}

	return &HostsSimulator{
		hosts:     hosts,
		allHosts:  hosts,
		hostIndex: hostCount,
		timeNowFn: timeNowFn,
	}
}

func (h *HostsSimulator) nextHostIndexWithLock() int {
	v := h.hostIndex
	h.hostIndex++
	return v
}

func (h *HostsSimulator) Hosts() []devops.Host {
	h.RLock()
	defer h.RUnlock()

	return append([]devops.Host{}, h.hosts...)
}

func (h *HostsSimulator) Generate(
	progressBy, scrapeDuration time.Duration,
	newSeriesPercent float64,
) (map[string][]prompb.TimeSeries, error) {
	h.Lock()
	defer h.Unlock()

	if newSeriesPercent < 0 || newSeriesPercent > 1 {
		return nil, fmt.Errorf(
			"newSeriesPercent not between [0.0,1.0]: value=%v",
			newSeriesPercent)
	}

	now := h.timeNowFn()
	factorProgress := float64(progressBy) / float64(scrapeDuration)
	numHosts := int(math.Ceil(factorProgress * float64(len(h.allHosts))))
	if numHosts == 0 {
		// Always progress by at least one
		numHosts = 1
	}
	if len(h.hosts) == 0 {
		// Out of hosts, remove/add hosts as needed and progress ticking
		for _, host := range h.allHosts {
			host.TickAll(progressBy)
		}
		if newSeriesPercent > 0 {
			remove := int(math.Ceil(newSeriesPercent * float64(len(h.allHosts))))
			h.allHosts = h.allHosts[:len(h.allHosts)-remove]
			for i := 0; i < remove; i++ {
				newHostIndex := h.nextHostIndexWithLock()
				newHost := devops.NewHost(newHostIndex, 0, now)
				h.allHosts = append(h.allHosts, newHost)
			}
		}
		// Reset hosts
		h.hosts = h.allHosts
	}
	if len(h.hosts) < numHosts {
		numHosts = len(h.hosts)
	}

	// Select hosts
	sendFromHosts := h.hosts[:numHosts]

	// Progress hosts
	h.hosts = h.hosts[numHosts:]

	nowUnixMilliseconds := now.UnixNano() / int64(time.Millisecond)

	hostValues := make(map[string][]prompb.TimeSeries)
	for _, host := range sendFromHosts {
		allSeries := make([]prompb.TimeSeries, 0, len(host.SimulatedMeasurements))
		for _, measurement := range host.SimulatedMeasurements {
			p := common.MakeUsablePoint()
			measurement.ToPoint(p)

			for i, fieldName := range p.FieldKeys {
				val := 0.0

				switch v := p.FieldValues[i].(type) {
				case int:
					val = float64(int(v))
				case int64:
					val = float64(int64(v))
				case float64:
					val = float64(v)
				default:
					panic(fmt.Sprintf("bad field %s with value type: %T with ", fieldName, v))
				}

				labels := []prompb.Label{
					prompb.Label{Name: labels.MetricName, Value: string(p.MeasurementName)},
					prompb.Label{Name: "measurement", Value: string(fieldName)},
					prompb.Label{Name: string(devops.MachineTagKeys[0]), Value: string(host.Name)},
					prompb.Label{Name: string(devops.MachineTagKeys[1]), Value: string(host.Region)},
					prompb.Label{Name: string(devops.MachineTagKeys[2]), Value: string(host.Datacenter)},
					prompb.Label{Name: string(devops.MachineTagKeys[3]), Value: string(host.Rack)},
					prompb.Label{Name: string(devops.MachineTagKeys[4]), Value: string(host.OS)},
					prompb.Label{Name: string(devops.MachineTagKeys[5]), Value: string(host.Arch)},
					prompb.Label{Name: string(devops.MachineTagKeys[6]), Value: string(host.Team)},
					prompb.Label{Name: string(devops.MachineTagKeys[7]), Value: string(host.Service)},
					prompb.Label{Name: string(devops.MachineTagKeys[8]), Value: string(host.ServiceVersion)},
					prompb.Label{Name: string(devops.MachineTagKeys[9]), Value: string(host.ServiceEnvironment)},
				}
				sample := prompb.Sample{
					Value:     val,
					Timestamp: nowUnixMilliseconds,
				}

				allSeries = append(allSeries, prompb.TimeSeries{
					Labels:  labels,
					Samples: []prompb.Sample{sample},
				})

			}
		}
		hostValues[string(host.Name)] = allSeries
	}

	return hostValues, nil
}
