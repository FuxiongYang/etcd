// Copyright 2015 The etcd Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"context"
	"fmt"
	"math"
	"os"
	"strconv"
	"time"

	v3 "go.etcd.io/etcd/clientv3"
	"go.etcd.io/etcd/pkg/report"

	"github.com/spf13/cobra"
	"golang.org/x/time/rate"
	"gopkg.in/cheggaaa/pb.v1"
)

// putCmd represents the put command
var putRealCmd = &cobra.Command{
	Use:   "real-put",
	Short: "Benchmark real-put",

	Run: putRealFunc,
}

var (
	realKeyPrefix string
	realKeyNumber int
	realBtreeDepth int
)

func init() {
	RootCmd.AddCommand(putRealCmd)
	putRealCmd.Flags().IntVar(&keySize, "key-size", 8, "Key size of put request")
	putRealCmd.Flags().IntVar(&valSize, "val-size", 8, "Value size of put request")
	putRealCmd.Flags().IntVar(&putRate, "rate", 0, "Maximum puts per second (0 is no limit)")

	putRealCmd.Flags().IntVar(&putTotal, "total", 10000, "Total number of put requests")
	putRealCmd.Flags().IntVar(&keySpaceSize, "key-space-size", 1, "Maximum possible keys")
	putRealCmd.Flags().BoolVar(&seqKeys, "sequential-keys", false, "Use sequential keys")
	putRealCmd.Flags().DurationVar(&compactInterval, "compact-interval", 0, `Interval to compact database (do not duplicate this with etcd's 'auto-compaction-retention' flag) (e.g. --compact-interval=5m compacts every 5-minute)`)
	putRealCmd.Flags().Int64Var(&compactIndexDelta, "compact-index-delta", 1000, "Delta between current revision and compact revision (e.g. current revision 10000, compact at 9000)")
	putRealCmd.Flags().BoolVar(&checkHashkv, "check-hashkv", false, "'true' to check hashkv")

	putRealCmd.Flags().StringVar(&realKeyPrefix, "real-key-prefix", "key", "prefix for input key")
	putRealCmd.Flags().IntVar(&realKeyNumber, "number-keys", 1, "prefix for input key")
	putRealCmd.Flags().IntVar(&realBtreeDepth, "keys-depth", 3, "the depth of the B-tree of etcd Key")

}

func putRealFunc(cmd *cobra.Command, args []string) {
	if keySpaceSize <= 0 {
		fmt.Fprintf(os.Stderr, "expected positive --key-space-size, got (%v)", keySpaceSize)
		os.Exit(1)
	}

	requests := make(chan v3.Op, totalClients)
	if putRate == 0 {
		putRate = math.MaxInt32
	}
	limit := rate.NewLimiter(rate.Limit(putRate), 1)
	clients := mustCreateClients(totalClients, totalConns)
	_, v := make([]byte, keySize), string(mustRandBytes(valSize))

	bar = pb.New(putTotal)
	bar.Format("Bom !")
	bar.Start()

	// 生成client
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

		for i := 0; i < putTotal; i++ {
			var key string
			leaf := "k8sKeyLeafNodeInEtcdDataTree"
			key = realKeyPrefix
			num := i % realKeyNumber
			for j := 0; j < realBtreeDepth; j++{
				key = key + "/" + leaf + strconv.Itoa(num)
			}
			key = key + "/k8sResourceDataName" + strconv.Itoa(num)
			requests <- v3.OpPut(key, v)
		}
		close(requests)
	}()

	if compactInterval > 0 {
		go func() {
			for {
				time.Sleep(compactInterval)
				compactKV(clients)
			}
		}()
	}

	rc := r.Run()
	wg.Wait()
	close(r.Results())
	bar.Finish()
	fmt.Println(<-rc)

	if checkHashkv {
		hashKV(cmd, clients)
	}
}
