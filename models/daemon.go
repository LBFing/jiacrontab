package models

import (
	"time"

	"github.com/jinzhu/gorm"
)

type DaemonJob struct {
	gorm.Model
	Name            string      `json:"name" gorm:"unique;not null"`
	UserID          uint        `json:"user_id"`
	ErrorMailNotify bool        `json:"errorMailNotify"`
	ErrorAPINotify  bool        `json:"errorAPINotify"`
	Disabled        bool        `json:"disabled"`
	Status          JobStatus   `json:"status"`
	MailTo          string      `json:"mailTo"`
	APITo           string      `json:"APITo"`
	FailRestart     bool        `json:"failRestart"`
	StartAt         time.Time   `json:"startAt"`
	WorkUser        string      `json:"workUser"`
	WorkEnv         StringSlice `json:"workEnv" gorm:"type:varchar(1000)"`
	WorkDir         string      `json:"workDir"`

	Commands StringSlice `json:"commands" gorm:"type:varchar(1000)"`
}