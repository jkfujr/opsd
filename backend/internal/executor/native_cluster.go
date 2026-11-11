package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/localstate"
)

type NativeIdentity struct {Username string `json:"username"`;Password string `json:"password"`;Cookie string `json:"cookie"`}
func ReadNativeIdentity(path string)(NativeIdentity,error){var v NativeIdentity;e:=localstate.Read(path,&v);if e==nil&&(v.Username==""||len(v.Password)<16||len(v.Cookie)<32||strings.ContainsAny(v.Username+v.Password+v.Cookie,"\r\n\x00")){e=fmt.Errorf("地域数据库身份无效")};return v,e}
func CreateNativeIdentity(path string)error{if _,e:=os.Stat(path);e==nil{_,e=ReadNativeIdentity(path);return e}else if !os.IsNotExist(e){return e};return localstate.Write(path,NativeIdentity{Username:"opsd_admin",Password:identity.Random(),Cookie:identity.Random()},0600)}
func PrepareNative(ctx context.Context,directory string,c cluster.StorageConfig,id NativeIdentity)error{c.Enabled=true;c.NativeCluster=true;path:=filepath.Join(directory,"couchdb.json");var state storageIdentity;e:=localstate.Read(path,&state);if e!=nil&&!os.IsNotExist(e){return e};entries,err:=os.ReadDir(filepath.Join(directory,"couchdb","data"));if err!=nil&&!os.IsNotExist(err){return err};if len(entries)>0&&(!state.Config.NativeCluster||state.Credentials.Username!=id.Username||state.Credentials.Password!=id.Password||state.Cookie!=id.Cookie){return fmt.Errorf("已有数据库不能切换地域身份或部署模式")};if len(entries)==0{state=storageIdentity{Credentials:cluster.Credentials{Username:id.Username,Password:id.Password},Cookie:id.Cookie};if e=localstate.Write(path,state,0600);e!=nil{return e}};s:=&Server{Directory:directory,prepare:true};_,e=s.storage(ctx,c);return e}
type nativeClient struct{HTTP *http.Client;Identity NativeIdentity}
func(c nativeClient)request(ctx context.Context,address,method,path string,body,out any)error{var r io.Reader;if body!=nil{data,e:=json.Marshal(body);if e!=nil{return e};r=bytes.NewReader(data)};request,e:=http.NewRequestWithContext(ctx,method,address+path,r);if e!=nil{return e};request.SetBasicAuth(c.Identity.Username,c.Identity.Password);request.Header.Set("Content-Type","application/json");response,e:=c.HTTP.Do(request);if e!=nil{return e};defer response.Body.Close();if response.StatusCode>=400{return fmt.Errorf("地域数据库配置失败（%d）",response.StatusCode)};if out!=nil{return json.NewDecoder(io.LimitReader(response.Body,1024*1024)).Decode(out)};_,e=io.Copy(io.Discard,io.LimitReader(response.Body,1024*1024));return e}
func InitializeNative(ctx context.Context,addresses []string,id NativeIdentity)error{if len(addresses)<3||len(addresses)>5{return fmt.Errorf("地域集群须提供 3 至 5 个同地域数据库地址")};client:=nativeClient{HTTP:&http.Client{Transport:&http.Transport{},Timeout:15*time.Second},Identity:id};defer client.HTTP.CloseIdleConnections();urls:=make([]*url.URL,len(addresses));for i,address:=range addresses{u,e:=url.Parse(address);if e!=nil||(u.Scheme!="http"&&u.Scheme!="https")||u.Hostname()==""||u.User!=nil||u.RawQuery!=""||u.Fragment!=""||u.Path!=""{return fmt.Errorf("地域数据库地址无效")};urls[i]=u;var state struct{State string `json:"state"`};if e=client.request(ctx,address,"GET","/_cluster_setup",nil,&state);e!=nil{return e};if state.State=="single_node_enabled"{return fmt.Errorf("不能将已有独立数据库自动转换为地域集群")};if state.State!="cluster_enabled"&&state.State!="cluster_finished"{body:=map[string]any{"action":"enable_cluster","bind_address":u.Hostname(),"username":id.Username,"password":id.Password,"node_count":len(addresses)};if e=client.request(ctx,address,"POST","/_cluster_setup",body,nil);e!=nil{return e}}}
	for i:=1;i<len(addresses);i++{var members struct{Nodes []string `json:"all_nodes"`};if e:=client.request(ctx,addresses[0],"GET","/_membership",nil,&members);e!=nil{return e};joined:=false;for _,node:=range members.Nodes{if node=="couchdb@"+urls[i].Hostname(){joined=true}};if joined{continue};port:=urls[i].Port();if port==""{port="5984"};if e:=client.request(ctx,addresses[0],"POST","/_cluster_setup",map[string]any{"action":"add_node","host":urls[i].Hostname(),"port":port,"username":id.Username,"password":id.Password},nil);e!=nil{return e}}
	var state struct{State string `json:"state"`};if e:=client.request(ctx,addresses[0],"GET","/_cluster_setup",nil,&state);e!=nil{return e};if state.State!="cluster_finished"{if e:=client.request(ctx,addresses[0],"POST","/_cluster_setup",map[string]string{"action":"finish_cluster"},nil);e!=nil{return e}};for _,address:=range addresses{if e:=client.request(ctx,address,"GET","/_up",nil,nil);e!=nil{return e}};return nil}
