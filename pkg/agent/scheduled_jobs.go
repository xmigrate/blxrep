package agent

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xmigrate/blxrep/utils"
)

type Scheduler struct {
	snapshotTime string
	frequency    utils.Frequency
	stopChan     chan struct{}
	wg           sync.WaitGroup
}

func NewScheduler(snapshotTime string, frequency utils.Frequency) (*Scheduler, error) {
	// Validate time format
	if _, err := time.Parse("15:04:00", snapshotTime); err != nil {
		return nil, fmt.Errorf("invalid time format. Use HH:MM:SS in 24-hour format: %v", err)
	}

	return &Scheduler{
		snapshotTime: snapshotTime,
		frequency:    frequency,
		stopChan:     make(chan struct{}),
	}, nil
}

func (s *Scheduler) Start(job func()) error {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		for {
			nextRun := s.calculateNextRun()
			utils.LogDebug(fmt.Sprintf("Next snapshot scheduled for: %v", nextRun))

			select {
			case <-time.After(time.Until(nextRun)):
				utils.LogDebug("Starting snapshot job")
				job()

			case <-s.stopChan:
				utils.LogDebug("Scheduler stopped")
				return
			}
		}
	}()

	return nil
}

func (s *Scheduler) Stop() {
	close(s.stopChan)
	s.wg.Wait()
}

func (s *Scheduler) calculateNextRun() time.Time {
	now := time.Now().UTC()
	parts := strings.Split(s.snapshotTime, ":")
	hour, _ := strconv.Atoi(parts[0])
	minute, _ := strconv.Atoi(parts[1])

	var scheduled time.Time

	switch s.frequency {
	case utils.Daily:
		// For daily, start from current day
		scheduled = time.Date(
			now.Year(), now.Month(), now.Day(),
			hour, minute, 0, 0, time.UTC,
		)
		if now.After(scheduled) {
			scheduled = scheduled.AddDate(0, 0, 1)
		}

	case utils.Weekly:
		// For weekly, start from the first day of the week (assuming Monday is first)
		scheduled = time.Date(
			now.Year(), now.Month(), now.Day(),
			hour, minute, 0, 0, time.UTC,
		)
		// Adjust to the most recent Monday
		for scheduled.Weekday() != time.Monday {
			scheduled = scheduled.AddDate(0, 0, -1)
		}
		if now.After(scheduled) {
			scheduled = scheduled.AddDate(0, 0, 7)
		}

	case utils.Monthly:
		// For monthly, always start from the first day of the month
		scheduled = time.Date(
			now.Year(), now.Month(), 1, // Use day 1 for first of month
			hour, minute, 0, 0, time.UTC,
		)
		if now.After(scheduled) {
			scheduled = time.Date(
				now.Year(), now.Month()+1, 1, // Move to first day of next month
				hour, minute, 0, 0, time.UTC,
			)
		}
	}

	return scheduled
}

func StartScheduledJobs(agentID string, snapshotURL string) error {
	scheduler, err := NewScheduler(utils.AgentConfiguration.SnapshotTime, utils.Frequency(utils.AgentConfiguration.SnapshotFreq))
	if err != nil {
		return fmt.Errorf("failed to create scheduler: %v", err)
	}
	utils.LogDebug(fmt.Sprintf("Scheduled job started to run at %s every %s", utils.AgentConfiguration.SnapshotTime, utils.AgentConfiguration.SnapshotFreq))
	// Start as a goroutine
	go func() {
		if err := scheduler.Start(func() {
			utils.LogDebug(fmt.Sprintf("Scheduled job %s: agent %s connecting to snapshot endpoint at %s", time.Now().Format(time.RFC3339), agentID, snapshotURL))
			if err := connectAndHandle(agentID, snapshotURL); err != nil {
				utils.LogError(fmt.Sprintf("Snapshot connection error: %v", err))
			}
			utils.LogDebug("Snapshot job is running")
		}); err != nil {
			utils.LogError(fmt.Sprintf("Scheduler error: %v", err))
		}
	}()

	return nil
}
