package activity

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/log"
	"gorm.io/gorm"

	"github.com/FishcakeLab/fishcake-service/common/enum"
	"github.com/FishcakeLab/fishcake-service/common/errors_h"
	"github.com/FishcakeLab/fishcake-service/config"
	_ "github.com/FishcakeLab/fishcake-service/database/utils/serializers"
	"github.com/FishcakeLab/fishcake-service/rpc/account"
	"github.com/FishcakeLab/fishcake-service/service/reward_service"
	"github.com/FishcakeLab/fishcake-service/service/rpc_service"
)

// ActivityParticipantAddress represents the participant's address information for an activity
type ActivityParticipantAddress struct {
	ActivityId int64  `gorm:"activity_id" json:"activityId"`
	Address    string `gorm:"address" json:"address"`
	JoinTime   int64  `gorm:"join_time" json:"joinTime"`
}

// ActivityInfo represents the main activity information
type ActivityInfo struct {
	Id                 string   `gorm:"id" json:"id"`
	ActivityId         int64    `gorm:"activity_id" json:"activityId"`
	BusinessAccount    string   `gorm:"business_account" json:"businessAccount"`
	BusinessName       string   `gorm:"business_name" json:"businessName"`
	ActivityContent    string   `gorm:"activity_content" json:"activityContent"`
	LatitudeLongitude  string   `gorm:"latitude_longitude" json:"latitudeLongitude"`
	ActivityCreateTime int64    `gorm:"activity_create_time" json:"activityCreateTime"`
	ActivityDeadline   int64    `gorm:"activity_deadline" json:"activityDeadline"`
	DropType           int8     `gorm:"drop_type" json:"dropType"`
	DropNumber         int64    `gorm:"drop_number" json:"dropNumber"`
	MinDropAmt         *big.Int `gorm:"serializer:u256;column:min_drop_amt" json:"minDropAmt"`
	MaxDropAmt         *big.Int `gorm:"serializer:u256;column:max_drop_amt" json:"maxDropAmt"`
	TokenContractAddr  string   `gorm:"token_contract_addr" json:"tokenContractAddr"`
	ActivityStatus     int8     `gorm:"activity_status" json:"activityStatus"`
	AlreadyDropNumber  int64    `gorm:"already_drop_number" json:"alreadyDropNumber"`
	BasicDeadline      uint64   `gorm:"basic_deadline" json:"basicDeadline" `
	ProDeadline        uint64   `gorm:"pro_deadline" json:"proDeadline"`
	ReturnAmount       *big.Int `gorm:"serializer:u256;column:return_amount" json:"returnAmount"`
	MinedAmount        *big.Int `gorm:"serializer:u256;column:mined_amount" json:"minedAmount"`
}

func (ActivityInfo) TableName() string {
	return "activity_info"
}

// ActivityInfoView defines the interface for viewing activity information
type ActivityInfoView interface {
	ActivityInfoList(activityFilter, businessAccount, activityStatus, businessName, tokenContractAddr, latitude, longitude, scope string,
		activityId,
		pageNum, pageSize int) ([]ActivityInfo, int)
	ActivityInfo(activityId int) ActivityInfo
}

// ActivityInfoDB defines the interface for activity database operations
type ActivityInfoDB interface {
	ActivityInfoView
	StoreActivityInfo(activityInfo ActivityInfo) error
	ActivityFinish(activityId string, ReturnAmount, MinedAmount *big.Int) error
	UpdateActivityInfo(activityId string) error
}

type activityInfoDB struct {
	db *gorm.DB
}

// UpdateActivityInfo increments the already_drop_number for an activity
func (a activityInfoDB) UpdateActivityInfo(activityId string) error {
	sql := `update activity_info set already_drop_number = already_drop_number + 1 where activity_id = ?`
	err := a.db.Exec(sql, activityId).Error
	return err
}

// ActivityFinish updates activity status and amounts when activity is finished
func (a activityInfoDB) ActivityFinish(activityId string, ReturnAmount, MinedAmount *big.Int) error {
	finishSql := `update activity_info set activity_status = 2, return_amount = ?, mined_amount = ? where activity_id = ?`
	err := a.db.Exec(finishSql, ReturnAmount, MinedAmount, activityId).Error
	return err
}

// StoreActivityInfo stores new activity information and handles reward distribution
func (a activityInfoDB) StoreActivityInfo(activityInfo ActivityInfo) error {
	activityInfoRecord := new(ActivityInfo)
	var exist ActivityInfo

	err := a.db.Table(activityInfoRecord.TableName()).Where("activity_id = ?", activityInfo.ActivityId).Take(&exist).Error
	var existAddress ActivityParticipantAddress

	errAdd := a.db.Table("activity_participants_addresses").
		Where("address = ?", activityInfo.BusinessAccount).
		Take(&existAddress).Error

	if errAdd != nil {
		if errors.Is(errAdd, gorm.ErrRecordNotFound) {
			fmt.Println(" has no activity_participants_addresses:")

			// Create new participant address record
			existAddress = ActivityParticipantAddress{
				ActivityId: activityInfo.ActivityId,
				Address:    activityInfo.BusinessAccount,
				JoinTime:   time.Now().Unix(),
			}

			// Get decryption key for reward distribution
			privateKey, err := reward_service.NewRewardService("").DecryptPrivateKey()
			if err != nil {
				log.Info("decrypt private key error: %v", err)
			}

			//	amount := new(big.Int).SetUint64(50)
			amount := new(big.Int).Mul(
				big.NewInt(50),
				new(big.Int).Exp(big.NewInt(10), big.NewInt(6), nil),
			)
			// Create and send transaction
			txHex, txHash, err := reward_service.NewRewardService("").CreateOfflineTransaction(
				reward_service.FCC,
				privateKey,
				activityInfo.BusinessAccount,
				amount,
			)
			log.Info(txHex, txHash)

			// Send transaction request
			req := &account.SendTxRequest{
				Chain:   "Polygon",
				Network: "mainnet",
				RawTx:   txHex,
			}
			cfg, err := config.New("./config.yaml")

			sendtx, _ := rpc_service.NewRpcService(cfg.RpcUrl).SendTx(context.Background(), req)

			if err != nil {
				log.Info("RPC send tx error: %v", err.Error())
			} else {
				log.Info("RPC send tx: %v", sendtx.String())
			}
			if err := a.db.Table("activity_participants_addresses").Create(&existAddress).Error; err != nil {
				return err
			}

		}
		fmt.Println(" has  activity_participants_addresses:")

	}
	if err != nil {
		fmt.Println(" has no activity_info:")

		if errors.Is(err, gorm.ErrRecordNotFound) {

			// Create new activity info record
			result := a.db.Table(activityInfoRecord.TableName()).
				Omit("id, basic_deadline, pro_deadline").
				Create(&activityInfo)
			return result.Error
		}
		return err
	}
	fmt.Println(" has  activity_info:")

	return nil
}

// ActivityInfo retrieves activity information by ID
func (a activityInfoDB) ActivityInfo(activityId int) ActivityInfo {
	var activityInfo ActivityInfo
	this := a.db.Table(ActivityInfo{}.TableName())
	this = this.Joins("LEFT JOIN account_nft_info ON activity_info.business_account = account_nft_info.address")
	this = this.Select("activity_info.*,account_nft_info.basic_deadline,account_nft_info.pro_deadline")
	result := this.Where("activity_id = ?", activityId).Take(&activityInfo)
	if result.Error == nil {
		return activityInfo
	} else if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		errors_h.NewErrorByEnum(enum.DataErr)
		return ActivityInfo{}
	} else {
		return ActivityInfo{}
	}
}

// ActivityInfoList retrieves a list of activities based on various filters
func (a activityInfoDB) ActivityInfoList(activityFilter, businessAccount, activityStatus, businessName, tokenContractAddr, latitude, longitude, scope string, activityId, pageNum, pageSize int) ([]ActivityInfo, int) {
	var activityInfo []ActivityInfo
	var count int64
	this := a.db.Table(ActivityInfo{}.TableName())

	// Apply filters
	if businessAccount != "" {
		this = this.Where("business_account ILIKE ?", "%"+businessAccount+"%")
	}
	if activityStatus != "" {
		this = this.Where("activity_status = ?", activityStatus)
		if activityStatus == "2" {
			this = this.Where("activity_deadline < ?", time.Now().Unix())
		} else {
			this = this.Where("activity_deadline > ?", time.Now().Unix())
		}
	}
	if businessName != "" {
		this = this.Where("business_name ILIKE ?", "%"+businessName+"%")
	}
	if tokenContractAddr != "" {
		this = this.Where("token_contract_addr ILIKE ?", "%"+tokenContractAddr+"%")
	}
	if activityId > 0 {
		this = this.Where("activity_id = ?", activityId)
	}
	if latitude != "" && longitude != "" {
		this = this.Where("ST_DWithin(ST_SetSRID(ST_MakePoint("+
			"CAST(SPLIT_PART(latitude_longitude, ',', 1) AS numeric), "+
			"CAST(SPLIT_PART(latitude_longitude, ',', 2) AS numeric)), 4326),ST_SetSRID(ST_MakePoint(?, ?), 4326),?)", latitude, longitude, scope)
	}
	if activityFilter == "1" {
		this = this.Where("account_nft_info.pro_deadline >= ?", time.Now().Unix())
	}
	if activityFilter == "2" {
		this = this.Where("account_nft_info.basic_deadline >= ?", time.Now().Unix())
	}

	// Join with account_nft_info table
	this = this.Joins("LEFT JOIN account_nft_info ON activity_info.business_account = account_nft_info.address")

	// Apply pagination
	if pageNum > 0 && pageSize > 0 {
		this = this.Limit(pageSize).Offset((pageNum - 1) * pageSize)
	}

	// Get total count and results
	this = this.Count(&count)
	this = this.Order("activity_create_time DESC, activity_status ASC").Select("activity_info.*,account_nft_info.basic_deadline,account_nft_info.pro_deadline")
	result := this.Find(&activityInfo)

	if result.Error == nil {
		return activityInfo, int(count)
	} else if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		errors_h.NewErrorByEnum(enum.DataErr)
		return nil, int(count)
	} else {
		return nil, int(count)
	}
}

// NewActivityDB creates a new instance of ActivityInfoDB
func NewActivityDB(db *gorm.DB) ActivityInfoDB {
	return &activityInfoDB{db: db}
}
