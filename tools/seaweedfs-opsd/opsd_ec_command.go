package command

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/seaweedfs/seaweedfs/weed/security"
	"github.com/seaweedfs/seaweedfs/weed/util"
	"github.com/seaweedfs/seaweedfs/weed/worker/tasks/erasure_coding"
)

func init() {
	Commands = append(Commands, &Command{
		UsageLine: "opsd-ec",
		Short:     "执行 OPSD 存储池 EC 步骤",
		Run:       runOpsdEC,
	})
}

func runOpsdEC(_ *Command, args []string) bool {
	var request erasure_coding.OPSDRequest
	reader := json.NewDecoder(io.LimitReader(os.Stdin, 1<<20))
	reader.DisallowUnknownFields()
	err := reader.Decode(&request)
	if err == nil && len(args) != 0 {
		err = fmt.Errorf("EC 步骤只接收标准输入")
	}
	if err == nil && reader.Decode(new(any)) != io.EOF {
		err = fmt.Errorf("EC 步骤输入有多余内容")
	}
	if err == nil {
		util.LoadSecurityConfiguration()
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
		defer cancel()
		var result any
		result, err = erasure_coding.RunOPSD(ctx, request, security.LoadClientTLS(util.GetViper(), "grpc.client"))
		if err == nil {
			err = json.NewEncoder(os.Stdout).Encode(result)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		SetCommandExitStatus(1)
	}
	return true
}
