package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	checkengine "github.com/ozgurcd/rulefloor/internal/check"
	"github.com/ozgurcd/rulefloor/internal/drift"
	"github.com/ozgurcd/rulefloor/internal/extract"
)

const diffContextLines = 3

func cmdDiff(repo, ruleID string, stdout io.Writer) error {
	result, err := drift.Compare(context.Background(), checkengine.ExecRunner{}, extract.DefaultRegistry(), repo, ruleID)
	if err != nil {
		return fatalf("CANNOT-EVALUATE: diff %s: %v", ruleID, err)
	}
	if result.ExpectedHash == result.ActualHash {
		fmt.Fprintf(stdout, "no drift %s: %s (hash %s)\n", result.RuleID, result.File, result.ActualHash)
		return nil
	}
	fmt.Fprintf(stdout, "drift %s: %s (ledger %s, working %s, baseline %.12s)\n", result.RuleID, result.File, result.ExpectedHash, result.ActualHash, result.BaselineRevision)
	writeBoundSpanDiff(stdout, result.BaselineBody, result.CurrentBody)
	return nil
}

func writeBoundSpanDiff(output io.Writer, before, after string) {
	oldLines := strings.Split(before, "\n")
	newLines := strings.Split(after, "\n")
	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(oldLines)-prefix && suffix < len(newLines)-prefix && oldLines[len(oldLines)-1-suffix] == newLines[len(newLines)-1-suffix] {
		suffix++
	}
	oldStart := max(0, prefix-diffContextLines)
	newStart := max(0, prefix-diffContextLines)
	oldEnd := min(len(oldLines), len(oldLines)-suffix+diffContextLines)
	newEnd := min(len(newLines), len(newLines)-suffix+diffContextLines)
	fmt.Fprintf(output, "@@ -%d,%d +%d,%d @@\n", oldStart+1, oldEnd-oldStart, newStart+1, newEnd-newStart)
	for _, line := range oldLines[oldStart:prefix] {
		fmt.Fprintf(output, " %s\n", line)
	}
	for _, line := range oldLines[prefix : len(oldLines)-suffix] {
		fmt.Fprintf(output, "-%s\n", line)
	}
	for _, line := range newLines[prefix : len(newLines)-suffix] {
		fmt.Fprintf(output, "+%s\n", line)
	}
	for _, line := range oldLines[len(oldLines)-suffix : oldEnd] {
		fmt.Fprintf(output, " %s\n", line)
	}
}
