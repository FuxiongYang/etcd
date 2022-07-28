package cmd

import (
	"context"
	"fmt"
	"github.com/spf13/cobra"
	v3 "go.etcd.io/etcd/clientv3"
	"go.etcd.io/etcd/pkg/report"
	"golang.org/x/time/rate"
	"gopkg.in/cheggaaa/pb.v1"
	"math"
	"os"
	"time"
)

var mutiRangeCmd = &cobra.Command{
	Use: "muti-range key [end-range]",
	Short: "Benchmark muti-range",

	Run: mutiRangeFunc,
}

func init() {
	RootCmd.AddCommand(mutiRangeCmd)
	mutiRangeCmd.Flags().IntVar(&rangeRate, "rate", 0, "Maximum range requests per second (0 is no limit)")
	mutiRangeCmd.Flags().IntVar(&rangeTotal, "total", 10000, "Total number of range requests")
	mutiRangeCmd.Flags().StringVar(&rangeConsistency, "consistency", "l", "Linearizable(l) or Serializable(s)")

	mutiRangeCmd.Flags().StringVar(&etcdConnectionConfig, "etcd-connection-config", "config.csv", "the record of etcd endpoints, the csv template title: 'endpoints,cert,key,cacert'")
	mutiRangeCmd.Flags().BoolVar(&ignoreCSVConfigFirstLine, "ignore-csv-title", false, "ignore the title in csv")
	mutiRangeCmd.Flags().UintVar(&etcdServerCount, "etcd-instance-count", 1024,"etcd instance count, default 1024")
}

func mutiRangeFunc(cmd *cobra.Command, args []string){
	if len(args) == 0 || len(args) > 2 {
		fmt.Fprintln(os.Stderr, cmd.Usage())
		os.Exit(1)
	}

	k := args[0]
	end := ""
	if len(args) == 2 {
		end = args[1]
	}

	if rangeConsistency == "l" {
		fmt.Println("bench with linearizable range")
	} else if rangeConsistency == "s" {
		fmt.Println("bench with serializable range")
	} else {
		fmt.Fprintln(os.Stderr, cmd.Usage())
		os.Exit(1)
	}

	if rangeRate == 0 {
		rangeRate = math.MaxInt32
	}
	limit := rate.NewLimiter(rate.Limit(rangeRate), 1)

	etcdInstances, _ := generateEtcdContextFromCSV(etcdServerCount, etcdConnectionConfig, ignoreCSVConfigFirstLine)
	requests := make(chan v3.Op, int(totalConns) * len(etcdInstances))
	clients := mustCreateClientsWithInput(totalClients, totalConns, etcdInstances)

	//requests := make(chan v3.Op, totalClients)
	//clients := mustCreateClients(totalClients, totalConns)

	bar = pb.New(rangeTotal)
	bar.Format("Bom !")
	bar.Start()

	r := newReport()
	for i := range clients {
		wg.Add(1)
		go func(c *v3.Client) {
			defer wg.Done()
			for op := range requests {
				limit.Wait(context.Background())

				st := time.Now()
				_, err := c.Do(context.Background(), op)
				r.Results() <- report.Result{Err: err, Start: st, End: time.Now()}
				bar.Increment()
			}
		}(clients[i])
	}

	go func() {
		for i := 0; i < rangeTotal; i++ {
			opts := []v3.OpOption{v3.WithRange(end)}
			if rangeConsistency == "s" {
				opts = append(opts, v3.WithSerializable())
			}
			op := v3.OpGet(k, opts...)
			requests <- op
		}
		close(requests)
	}()

	rc := r.Run()
	wg.Wait()
	close(r.Results())
	bar.Finish()
	fmt.Printf("%s", <-rc)
}