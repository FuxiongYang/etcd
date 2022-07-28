package cmd

import (
	"encoding/csv"
	"fmt"
	"go.etcd.io/etcd/clientv3"
	"go.etcd.io/etcd/pkg/transport"
	"google.golang.org/grpc/grpclog"
	"io"
	"os"
	"strings"
)

var etcdConfigMap map[string]interface{}

func mustCreateConnWithInput(clusterEndpoints []string, clusterTls transport.TLSInfo) *clientv3.Client{
	connEndpoints := leaderEps
	if len(connEndpoints) == 0 {
		connEndpoints = []string{clusterEndpoints[dialTotal%len(endpoints)]}
		dialTotal++
	}
	cfg := clientv3.Config{
		Endpoints:   connEndpoints,
		DialTimeout: dialTimeout,
	}
	if !clusterTls.Empty() || clusterTls.TrustedCAFile != "" {
		cfgtls, err := clusterTls.ClientConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "bad tls config: %v\n", err)
			os.Exit(1)
		}
		cfg.TLS = cfgtls
	}

	//if len(user) != 0 {
	//	username, password, err := getUsernamePassword(user)
	//	if err != nil {
	//		fmt.Fprintf(os.Stderr, "bad user information: %s %v\n", user, err)
	//		os.Exit(1)
	//	}
	//	cfg.Username = username
	//	cfg.Password = password
	//
	//}

	client, err := clientv3.New(cfg)
	if targetLeader && len(leaderEps) == 0 {
		mustFindLeaderEndpoints(client)
		client.Close()
		return mustCreateConn()
	}

	clientv3.SetLogger(grpclog.NewLoggerV2(os.Stderr, os.Stderr, os.Stderr))

	if err != nil {
		fmt.Fprintf(os.Stderr, "dial error: %v\n", err)
		os.Exit(1)
	}

	return client
}

func mustCreateClientsWithInput(totalClients, totalConns uint, etcdInstances []*etcdConfigCSVContext) []*clientv3.Client {
	connsCount := int(totalConns) * len(etcdInstances)
	conns := make([]*clientv3.Client, connsCount)
	for i := range conns {
		conns[i] = mustCreateConnWithInput(etcdInstances[i % len(etcdInstances)].endpoints, etcdInstances[i % len(etcdInstances)].tls)
	}

	clients := make([]*clientv3.Client, int(totalClients) * len(etcdInstances))
	for i := range clients {
		clients[i] = conns[i%connsCount]
	}
	return clients
}

func readCSVLine(dataCount uint, path string, isIgnoreFirstLine bool) ([][]string, error){
	var csvMsgs = make([][]string, dataCount)
	var lineCount int

	file, err := os.Open(path)
	if err != nil {return nil, err}
	defer func(){_ = file.Close()}()

	csvReader := csv.NewReader(file)
	csvReader.Comment = '#'

	for index := range csvMsgs{
		line, rerr := csvReader.Read()
		if rerr != nil && rerr != io.EOF { return nil, err }
		if rerr == io.EOF { break }
		lineCount = index
		csvMsgs[index] = line
	}

	if isIgnoreFirstLine{
		return csvMsgs[1:(lineCount+1)], nil
	}
	return csvMsgs[:(lineCount+1)], nil
}

func splitStringWithDelimiter(in string, delimiter string)[]string {
	return strings.Split(in, delimiter)
}

func openConfigFile(path string) []byte {
	file, err := os.Open(path)
	defer func(){_ = file.Close()}()
	if err != nil{
		return nil
	}
	data := make([]byte, 4096)

	count, err := file.Read(data)
	if err != nil{
		return nil
	}
	return data[:count]
}

