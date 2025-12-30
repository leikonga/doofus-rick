package cron

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

type CronValue struct {
	Any   bool
	Value int
}

type CronSchedule struct {
	Minute  CronValue
	Hour    CronValue
	Day     CronValue
	Month   CronValue
	Weekday CronValue
}

type Parse struct {
	Input string
}

func (p *Parse) trim() {
	p.Input = strings.TrimLeftFunc(p.Input, unicode.IsSpace)
}

func (p *Parse) parseWildcard() bool {
	isWildcard := p.Input[0] == '*'

	if isWildcard {
		p.Input = p.Input[1:]
	}

	return isWildcard
}

func (p *Parse) parseDigits() (int, error) {
	i := 0
	for i < len(p.Input) {
		if !unicode.IsDigit(rune(p.Input[i])) {
			break
		}
		i++
	}

	digits := p.Input[:i]
	p.Input = p.Input[i:]

	if digits == "" {
		return 0, errors.New("invalid digits")
	}

	n, err := strconv.Atoi(digits)
	if err != nil {
		return 0, errors.New("invalid integer")
	}

	return n, nil
}

func (p *Parse) expectSpace() error {
	if p.Input == "" {
		return errors.New("expected whitespace but got EOF")
	}

	if p.Input[0] != ' ' {
		return fmt.Errorf("expected whitespace but got %c", p.Input[0])
	}

	p.trim()
	return nil
}

func (p *Parse) parseCronValue(validator func(value int) error) (CronValue, error) {
	p.trim()
	if p.Input == "" {
		return CronValue{}, errors.New("unexpected EOF")
	}

	if p.parseWildcard() {
		return CronValue{Any: true}, nil
	}

	digits, err := p.parseDigits()
	if err != nil {
		return CronValue{}, err
	}

	if err = validator(digits); err != nil {
		return CronValue{}, err
	}

	return CronValue{Value: digits}, nil
}

func (p *Parse) ParseMinute() (CronValue, error) {
	return p.parseCronValue(func(value int) error {
		if value < 0 || value > 59 {
			return errors.New("0 <= minute <= 59 not satisfied")
		}
		return nil
	})
}

func (p *Parse) ParseHour() (CronValue, error) {
	return p.parseCronValue(func(value int) error {
		if value < 0 || value > 23 {
			return errors.New("0 <= hour <= 23 not satisfied")
		}
		return nil
	})
}

func (p *Parse) ParseDay() (CronValue, error) {
	return p.parseCronValue(func(value int) error {
		if value < 0 || value > 31 {
			return errors.New("0 <= day <= 31 not satisfied")
		}
		return nil
	})
}

func (p *Parse) ParseMonth() (CronValue, error) {
	return p.parseCronValue(func(value int) error {
		if value < 0 || value > 12 {
			return errors.New("0 <= month <= 12 not satisfied")
		}
		return nil
	})
}

func (p *Parse) ParseWeekday() (CronValue, error) {
	return p.parseCronValue(func(value int) error {
		if value < 0 || value > 6 {
			return errors.New("0 <= weekday <= 6 not satisfied")
		}
		return nil
	})
}

func (p *Parse) RunParse() (CronSchedule, error) {
	minute, err := p.ParseMinute()
	if err != nil {
		return CronSchedule{}, err
	}

	if err = p.expectSpace(); err != nil {
		return CronSchedule{}, fmt.Errorf("minute: %w", err)
	}

	hour, err := p.ParseHour()
	if err != nil {
		return CronSchedule{}, err
	}

	if err = p.expectSpace(); err != nil {
		return CronSchedule{}, fmt.Errorf("hour: %w", err)
	}

	day, err := p.ParseDay()
	if err != nil {
		return CronSchedule{}, err
	}

	if err = p.expectSpace(); err != nil {
		return CronSchedule{}, fmt.Errorf("day: %w", err)
	}

	month, err := p.ParseMonth()
	if err != nil {
		return CronSchedule{}, err
	}

	if err = p.expectSpace(); err != nil {
		return CronSchedule{}, fmt.Errorf("month: %w", err)
	}

	weekday, err := p.ParseWeekday()
	if err != nil {
		return CronSchedule{}, err
	}

	schedule := CronSchedule{
		Minute:  minute,
		Hour:    hour,
		Day:     day,
		Month:   month,
		Weekday: weekday,
	}
	return schedule, nil
}
