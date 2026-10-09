package admin

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"text/tabwriter"

	"cloudchat/internal/stats"
)

// showStats prints one line per day: archived days from STATS_FILE plus the
// days still in Redis (today included). `stats archive` archives finished
// days right away instead of waiting for the server's hourly run.
func showStats(args []string, out io.Writer, opts Options) error {
	if opts.StatsFile == "" {
		return errors.New("statistics are off (STATS_FILE is empty)")
	}
	archive := stats.Archive{Path: opts.StatsFile}
	days := 30
	if len(args) > 0 {
		if args[0] == "archive" {
			dates, err := archive.Rollover()
			if err != nil {
				return err
			}
			if len(dates) == 0 {
				fmt.Fprintln(out, "Nothing to archive.")
			} else {
				fmt.Fprintf(out, "Archived %v to %s.\n", dates, opts.StatsFile)
			}
			return nil
		}
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 1 {
			return fmt.Errorf("stats: %q is not a number of days", args[0])
		}
		days = n
	}
	list, err := archive.Recent(days)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Last %d day(s), UTC. Today is still counting; finished days are in %s.\n\n", len(list), opts.StatsFile)
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "DATE\tVISITORS\tROOMS\tJOINS\tMESSAGES\tFILES\tUPLOAD MB\tDOWNLOADS\tDOWNLOAD MB\tSECRETS\tREAD\tPEAK ONLINE\tPEAK ROOMS\tLIMITED\tFULL\t")
	var sum stats.Day
	sum.Counts = map[string]int64{}
	for _, d := range list {
		c := d.Counts
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%d\t%s\t%d\t%s\t%d\t%d\t%d\t%d\t%d\t%d\t\n",
			d.Date, d.Visitors, c[stats.RoomsCreated], c[stats.RoomJoins], c[stats.Messages],
			c[stats.FilesUploaded], mb(c[stats.FileBytes]), c[stats.FilesDownloaded], mb(c[stats.DownloadBytes]),
			c[stats.SecretsCreated], c[stats.SecretsRead], c[stats.PeakOnline], c[stats.PeakRooms],
			c[stats.RateLimited], c[stats.StorageRejected])
		sum.Visitors += d.Visitors
		for k, v := range c {
			if k == stats.PeakOnline || k == stats.PeakRooms {
				sum.Counts[k] = max(sum.Counts[k], v)
			} else {
				sum.Counts[k] += v
			}
		}
	}
	if len(list) > 1 {
		c := sum.Counts
		fmt.Fprintf(tw, "total\t%d\t%d\t%d\t%d\t%d\t%s\t%d\t%s\t%d\t%d\t%d\t%d\t%d\t%d\t\n",
			sum.Visitors, c[stats.RoomsCreated], c[stats.RoomJoins], c[stats.Messages],
			c[stats.FilesUploaded], mb(c[stats.FileBytes]), c[stats.FilesDownloaded], mb(c[stats.DownloadBytes]),
			c[stats.SecretsCreated], c[stats.SecretsRead], c[stats.PeakOnline], c[stats.PeakRooms],
			c[stats.RateLimited], c[stats.StorageRejected])
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(out, "\nVISITORS is distinct client networks per day (summing days counts repeat visitors again). PEAK ONLINE is per server.")
	return nil
}

func mb(b int64) string {
	if b == 0 {
		return "0"
	}
	return strconv.FormatFloat(float64(b)/(1<<20), 'f', 1, 64)
}
