// SPDX-License-Identifier: MIT

package processor

import (
	jsoniter "github.com/json-iterator/go"
)

func addLanguagePercentages(language []LanguageSummary) {
	var sumFiles, sumLines, sumCode, sumComment, sumBlank, sumComplexity, sumBytes int64
	for _, l := range language {
		sumFiles += l.Count
		sumLines += l.Lines
		sumCode += l.Code
		sumComment += l.Comment
		sumBlank += l.Blank
		sumComplexity += l.Complexity
		sumBytes += l.Bytes
	}

	percent := func(value, total int64) *float64 {
		var p float64
		if total != 0 {
			p = float64(value) / float64(total) * 100
		}
		return &p
	}

	for i := range language {
		language[i].FilePercent = percent(language[i].Count, sumFiles)
		language[i].LinePercent = percent(language[i].Lines, sumLines)
		language[i].CodePercent = percent(language[i].Code, sumCode)
		language[i].CommentPercent = percent(language[i].Comment, sumComment)
		language[i].BlankPercent = percent(language[i].Blank, sumBlank)
		language[i].ComplexityPercent = percent(language[i].Complexity, sumComplexity)
		language[i].BytePercent = percent(language[i].Bytes, sumBytes)
	}
}

func toJSON(input chan *FileJob) string {
	startTime := makeTimestampMilli()
	language := aggregateLanguageSummary(input)
	language = sortLanguageSummary(language)

	if Percent {
		addLanguagePercentages(language)
	}

	json := jsoniter.ConfigCompatibleWithStandardLibrary
	jsonString, _ := json.Marshal(language)

	printDebugF("milliseconds to build formatted string: %d", makeTimestampMilli()-startTime)

	return string(jsonString)
}

type Json2 struct {
	LanguageSummary         []LanguageSummary `json:"languageSummary"`
	EstimatedCost           float64           `json:"estimatedCost"`
	EstimatedScheduleMonths float64           `json:"estimatedScheduleMonths"`
	EstimatedPeople         float64           `json:"estimatedPeople"`
}

func toJSON2(input chan *FileJob) string {
	startTime := makeTimestampMilli()
	language := aggregateLanguageSummary(input)
	language = sortLanguageSummary(language)

	if Percent {
		addLanguagePercentages(language)
	}

	var sumCode int64
	for _, l := range language {
		sumCode += l.Code
	}

	cost, schedule, people := esstimateCostScheduleMonths(sumCode)

	j2 := Json2{
		LanguageSummary:         language,
		EstimatedCost:           cost,
		EstimatedScheduleMonths: schedule,
		EstimatedPeople:         people,
	}

	json := jsoniter.ConfigCompatibleWithStandardLibrary
	jsonString, _ := json.Marshal(j2)

	printDebugF("milliseconds to build formatted string: %d", makeTimestampMilli()-startTime)

	return string(jsonString)
}
