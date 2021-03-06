package cache_mgr

import (
	"context"
	"database/sql"
	"gen/proto"
	"github.com/go-redis/redis"
	"google.golang.org/grpc"
	"gosconf"
	"goslib/logger"
	"goslib/mysqldb"
	"goslib/redisdb"
	"net"
	"strings"
	"time"
)

var grpcServer *grpc.Server

type CacheMgr struct {
}

const CACHE_EXPIRE = 1 * time.Hour

func Start() {
	StartPersister()

	conf := gosconf.RPC_FOR_CACHE_MGR
	lis, err := net.Listen(conf.ListenNet, net.JoinHostPort("", conf.ListenPort))
	logger.INFO("CacheRpcServer lis: ", conf.ListenNet, " port: ", conf.ListenPort)
	if err != nil {
		logger.ERR("failed to listen: ", err)
	}

	grpcServer = grpc.NewServer()
	proto.RegisterCacheRpcServerServer(grpcServer, &CacheMgr{})

	err = mysqldb.StartClient()
	if err != nil {
		logger.ERR("Start CacheRpcServer failed: ", err)
		panic(err)
	}

	go func() {
		err := grpcServer.Serve(lis)
		if err != nil {
			logger.ERR("Start CacheRpcServer failed: ", err)
			panic(err)
		}
	}()
}

func Stop() {
	grpcServer.GracefulStop()
	EnsurePersistered()
}

func (self *CacheMgr) Take(ctx context.Context, in *proto.TakeRequest) (*proto.TakeReply, error) {
	logger.INFO("cache Take: ", in.PlayerId)
	content, err := getFromRedis(in.PlayerId)
	if err == redis.Nil {
		content, err = getFromMySQL(in.PlayerId)
		if err == sql.ErrNoRows {
			return &proto.TakeReply{}, nil
		}
		if err != nil {
			logger.ERR("Take PlayerData query MySQL failed: ", err)
			return nil, err
		}
		return &proto.TakeReply{Data: content}, nil
	}

	if err != nil {
		logger.ERR("Take PlayerData from redis failed: ", in.PlayerId, err)
		return nil, err
	}

	if err = delFromRedis(in.PlayerId); err != nil {
		logger.ERR("cache_mgr del from redis failed: ", err)
	}

	return &proto.TakeReply{Data: content}, nil
}

func (self *CacheMgr) Return(ctx context.Context, in *proto.ReturnRequest) (*proto.ReturnReply, error) {
	logger.INFO("cache Return: ", in.PlayerId)
	if err := persistToRedis(in.PlayerId, in.Data); err != nil {
		logger.ERR("Return PlayerData failed: ", in.PlayerId, err)
		return &proto.ReturnReply{Success: false}, err
	}

	persistToMySQL(in.PlayerId, in.Data, in.Version, true)

	return &proto.ReturnReply{Success: true}, nil
}

func (self *CacheMgr) Persist(ctx context.Context, in *proto.PersistRequest) (*proto.PersistReply, error) {
	logger.INFO("cache Persist: ", in.PlayerId)
	persistToMySQL(in.PlayerId, in.Data, in.Version, false)
	return &proto.PersistReply{Success: true}, nil
}

func cacheKey(playerId string) string {
	return strings.Join([]string{"player_data", playerId}, ":")
}

func getFromMySQL(playerId string) (string, error) {
	var content string
	query := "SELECT content FROM player_datas WHERE uuid=?"
	err := mysqldb.Instance().QueryRow(query, playerId).Scan(&content)
	return content, err
}

func persistToRedis(playerId, content string) error {
	key := cacheKey(playerId)
	_, err := redisdb.Instance().Set(key, content, 0).Result()
	return err
}

func getFromRedis(playerId string) (string, error) {
	key := cacheKey(playerId)
	return redisdb.Instance().Get(key).Result()
}

func delFromRedis(playerId string) error {
	key := cacheKey(playerId)
	_, err := redisdb.Instance().Del(key).Result()
	return err
}
