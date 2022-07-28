package cmd

import (
	"context"
	"fmt"
	"github.com/spf13/cobra"
	v3 "go.etcd.io/etcd/clientv3"
	"go.etcd.io/etcd/pkg/report"
	"go.etcd.io/etcd/pkg/transport"
	"golang.org/x/time/rate"
	"gopkg.in/cheggaaa/pb.v1"
	"math"
	"os"
	"strconv"
	"time"
)

var mutiPutCmd = &cobra.Command{
	Use:   "muti-put",
	Short: "Benchmark muti-put",

	Run: mutiPutFunc,
}

var (
	etcdConnectionConfig string
	ignoreCSVConfigFirstLine bool
	etcdServerCount uint
)

func init() {
	RootCmd.AddCommand(mutiPutCmd)
	mutiPutCmd.Flags().IntVar(&keySize, "key-size", 8, "Key size of put request")
	mutiPutCmd.Flags().IntVar(&valSize, "val-size", 8, "Value size of put request")
	mutiPutCmd.Flags().IntVar(&putRate, "rate", 0, "Maximum puts per second (0 is no limit)")

	mutiPutCmd.Flags().IntVar(&putTotal, "total", 10000, "Total number of put requests")
	mutiPutCmd.Flags().IntVar(&keySpaceSize, "key-space-size", 1, "Maximum possible keys")
	mutiPutCmd.Flags().BoolVar(&seqKeys, "sequential-keys", false, "Use sequential keys")
	mutiPutCmd.Flags().DurationVar(&compactInterval, "compact-interval", 0, `Interval to compact database (do not duplicate this with etcd's 'auto-compaction-retention' flag) (e.g. --compact-interval=5m compacts every 5-minute)`)
	mutiPutCmd.Flags().Int64Var(&compactIndexDelta, "compact-index-delta", 1000, "Delta between current revision and compact revision (e.g. current revision 10000, compact at 9000)")
	mutiPutCmd.Flags().BoolVar(&checkHashkv, "check-hashkv", false, "'true' to check hashkv")

	mutiPutCmd.Flags().StringVar(&realKeyPrefix, "real-key-prefix", "key", "prefix for input key")
	mutiPutCmd.Flags().IntVar(&realKeyNumber, "number-keys", 1, "prefix for input key")
	mutiPutCmd.Flags().IntVar(&realBtreeDepth, "keys-depth", 3, "the depth of the B-tree of etcd Key")

	mutiPutCmd.Flags().StringVar(&etcdConnectionConfig, "etcd-connection-config", "config.csv", "the record of etcd endpoints, the csv template title: 'endpoints,cert,key,cacert'")
	mutiPutCmd.Flags().BoolVar(&ignoreCSVConfigFirstLine, "ignore-csv-title", false, "ignore the title in csv")
	mutiPutCmd.Flags().UintVar(&etcdServerCount, "etcd-instance-count", 1024,"etcd instance count, default 1024")
}

func mutiPutFunc(cmd *cobra.Command, args []string){
	if keySpaceSize <= 0 {
		fmt.Fprintf(os.Stderr, "expected positive --key-space-size, got (%v)", keySpaceSize)
		os.Exit(1)
	}

	requests := make(chan v3.Op, totalClients)
	if putRate == 0 {
		putRate = math.MaxInt32
	}
	limit := rate.NewLimiter(rate.Limit(putRate), 1)

	etcdInstances, _ := generateEtcdContextFromCSV(etcdServerCount, etcdConnectionConfig, ignoreCSVConfigFirstLine)
	clients := mustCreateClientsWithInput(totalClients, totalConns, etcdInstances)

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

func generateEtcdContextFromCSV(dataCount uint, path string, isIgnoreFirstLine bool)([]*etcdConfigCSVContext, error){
	csvDatas, err := readCSVLine(dataCount, path, isIgnoreFirstLine)
	if err != nil { return nil, err }
	if len(csvDatas) < 1{return nil, fmt.Errorf("no instance need to test")}

	var etcdConfigs = make([]*etcdConfigCSVContext,len(csvDatas))
	for index, val := range csvDatas{
		etcdConfig := new(etcdConfigCSVContext)
		etcdConfig.generateContextFromCSV(val)
		etcdConfigs[index] = etcdConfig
	}
	return etcdConfigs, nil
}

type etcdConfigCSVContext struct{
	endpoints []string
	tls transport.TLSInfo
}

func (e *etcdConfigCSVContext) generateContextFromCSV(csvData []string) {
	e.endpoints = splitStringWithDelimiter(csvData[0], "|")
	e.tls = transport.TLSInfo{
		CertFile: csvData[1],
		KeyFile: csvData[2],
		TrustedCAFile: csvData[3],
	}
}

