package jiacrontabd

import (
	"errors"
	"fmt"
	"jiacrontab/models"
	"jiacrontab/pkg/crontab"
	"jiacrontab/pkg/finder"
	"jiacrontab/pkg/proto"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/iwannay/log"
)

type Srv struct {
}

func (s *Srv) Ping(args *proto.EmptyArgs, reply *proto.EmptyReply) error {
	return nil
}

type CrontabJob struct {
	jd *Jiacrontabd
}

func newCrontabJobSrv(d *Jiacrontabd) *CrontabJob {
	return &CrontabJob{
		jd: d,
	}
}

func (j *CrontabJob) List(args proto.QueryJobArgs, reply *proto.QueryCrontabJobRet) error {
	err := models.DB().Model(&models.CrontabJob{}).Count(&reply.Total).Error
	if err != nil {
		return err
	}

	reply.Page = args.Page
	reply.Pagesize = args.Pagesize

	return models.DB().Offset(args.Page - 1).Limit(args.Pagesize).Find(&reply.List).Error
}

func (j *CrontabJob) Audit(args proto.AuditJobArgs, reply *bool) error {
	*reply = true
	return models.DB().Model(&models.CrontabJob{}).Where("id in(?) and status=?", args.JobIDs, models.StatusJobUnaudited).Update("status", models.StatusJobOk).Error
}

func (j *CrontabJob) Edit(args models.CrontabJob, rowsAffected *int64) error {

	var (
		db *models.D
	)

	log.Debug(args)

	if args.MaxConcurrent == 0 {
		args.MaxConcurrent = 1
	}

	if args.ID == 0 {
		db = models.DB().Create(&args)
		*rowsAffected = db.RowsAffected
		return db.Error
	}

	if args.ID == 0 {
		db = models.DB().Save(&args)
	} else {
		db = models.DB().Where("id=?", args.ID).Debug().Omit(
			"updated_at", "created_at", "deleted_at",
			"createdUserID", "createdUsername",
			"last_cost_time", "last_exec_time",
			"next_exec_time", "last_exit_status", "process_num",
			"status",
		).Save(&args)
	}

	*rowsAffected = db.RowsAffected
	return db.Error
}
func (j *CrontabJob) Get(args uint, reply *models.CrontabJob) error {
	return models.DB().Find(reply, "id=?", args).Error
}

func (j *CrontabJob) Start(ids []uint, ok *bool) error {
	var (
		jobs []models.CrontabJob
	)

	*ok = true

	if len(ids) == 0 {
		return errors.New("empty ids")
	}

	ret := models.DB().Find(&jobs, "id in (?) and status in (?)", ids, []models.JobStatus{models.StatusJobOk, models.StatusJobStop})
	if ret.Error != nil {
		return ret.Error
	}

	for _, v := range jobs {
		j.jd.addJob(&crontab.Job{
			ID:      v.ID,
			Second:  v.TimeArgs.Second,
			Minute:  v.TimeArgs.Minute,
			Hour:    v.TimeArgs.Hour,
			Day:     v.TimeArgs.Day,
			Month:   v.TimeArgs.Month,
			Weekday: v.TimeArgs.Weekday,
		})
	}

	return nil
}

func (j *CrontabJob) Stop(ids []uint, ok *bool) error {
	*ok = true
	return models.DB().Model(&models.CrontabJob{}).Where("id in (?)", ids).Update("status", models.StatusJobStop).Error
}

func (j *CrontabJob) Delete(ids []uint, ok *bool) error {
	*ok = true
	return models.DB().Where("id in (?)", ids).Delete(&models.CrontabJob{}).Error
}

func (j *CrontabJob) Kill(jobIDs []uint, ok *bool) error {
	for _, jobID := range jobIDs {
		j.jd.killTask(jobID)
	}
	return nil
}

func (j *CrontabJob) Exec(jobID uint, reply *[]byte) error {

	var job models.CrontabJob
	ret := models.DB().Debug().Find(&job, "id=?", jobID)

	if ret.Error == nil {
		jobInstance := newJobEntry(&crontab.Job{
			ID:    job.ID,
			Value: job,
		}, j.jd)

		j.jd.addTmpJob(jobInstance)
		defer j.jd.removeTmpJob(jobInstance)

		jobInstance.once = true
		jobInstance.exec()

		*reply = jobInstance.waitDone()

	} else {
		*reply = []byte("failed to start")
	}
	return nil

}

func (j *CrontabJob) Log(args proto.SearchLog, reply *proto.SearchLogResult) error {

	fd := finder.NewFinder(1000000, func(info os.FileInfo) bool {
		basename := filepath.Base(info.Name())
		arr := strings.Split(basename, ".")
		if len(arr) != 2 {
			return false
		}

		if arr[1] == "log" && arr[0] == fmt.Sprint(args.JobID) {
			return true
		}
		return false
	})

	if args.Date == "" {
		args.Date = time.Now().Format("2006/01/02")
	}
	if args.IsTail {
		fd.SetTail(true)
	}

	rootpath := filepath.Join(cfg.LogPath, "crontab_task", args.Date)
	err := fd.Search(rootpath, args.Pattern, &reply.Content, args.Page, args.Pagesize)
	reply.Total = int(fd.Count())
	return err

}

// SetDependDone 依赖执行完毕时设置相关状态
func (j *CrontabJob) SetDependDone(args proto.DepJob, reply *bool) error {
	*reply = j.jd.SetDependDone(&depEntry{
		jobID:       args.JobID,
		processID:   args.ProcessID,
		jobUniqueID: args.JobUniqueID,
		dest:        args.Dest,
		from:        args.From,
		done:        true,
		logContent:  args.LogContent,
		err:         args.Err,
	})
	return nil
}

// ExecDepend 执行依赖
func (j *CrontabJob) ExecDepend(args proto.DepJob, reply *bool) error {
	j.jd.dep.add(&depEntry{
		jobUniqueID: args.JobUniqueID,
		processID:   args.ProcessID,
		jobID:       args.JobID,
		id:          args.ID,
		dest:        args.Dest,
		from:        args.From,
		name:        args.Name,
		commands:    args.Commands,
	})
	*reply = true
	log.Infof("job %s %v add to execution queue ", args.Name, args.Commands)
	return nil
}

func (j *CrontabJob) Ping(args *proto.EmptyArgs, reply *proto.EmptyReply) error {
	return nil
}

type DaemonJob struct {
	jd *Jiacrontabd
}

func newDaemonJobSrv(jd *Jiacrontabd) *DaemonJob {
	return &DaemonJob{
		jd: jd,
	}
}

func (j *DaemonJob) List(args proto.QueryJobArgs, reply *proto.QueryDaemonJobRet) error {
	err := models.DB().Model(&models.DaemonJob{}).Count(&reply.Total).Error
	if err != nil {
		return err
	}

	reply.Page = args.Page
	reply.Pagesize = args.Pagesize

	return models.DB().Offset(args.Page - 1).Limit(args.Pagesize).Find(&reply.List).Error
}

func (j *DaemonJob) Edit(args models.DaemonJob, reply *int64) error {

	if args.ID == 0 {
		ret := models.DB().Create(&args)
		*reply = ret.RowsAffected
		return ret.Error
	}

	ret := models.DB().Where("id=?", args.ID).Save(&args)
	*reply = ret.RowsAffected

	return ret.Error
}

func (j *DaemonJob) ListDaemonJob(args proto.QueryJobArgs, reply *[]models.DaemonJob) error {
	return models.DB().Find(reply).Offset((args.Page - 1) * args.Pagesize).Limit(args.Pagesize).Order("update_at desc").Error
}

func (j *DaemonJob) Start(jobIDs []uint, reply *bool) error {

	var jobs []models.DaemonJob

	*reply = true

	ret := models.DB().Find(&jobs, "id in(?) and status in (?)", jobIDs, []models.JobStatus{models.StatusJobOk, models.StatusJobStop})

	if ret.Error != nil {
		return ret.Error
	}

	for _, v := range jobs {
		job := v
		j.jd.daemon.add(&daemonJob{
			job: &job,
		})
	}

	return nil
}

func (j *DaemonJob) Stop(jobIDs []uint, reply *bool) error {

	*reply = true

	for _, id := range jobIDs {
		j.jd.daemon.PopJob(id)
	}

	ret := models.DB().Model(&models.DaemonJob{}).Where("id in(?)", jobIDs).Update("status", models.StatusJobStop)

	if ret.Error != nil {
		return ret.Error
	}

	return nil
}

func (j *DaemonJob) Delete(jobIDs []uint, reply *bool) error {

	*reply = true

	for _, id := range jobIDs {
		j.jd.daemon.PopJob(id)
	}
	ret := models.DB().Delete(&models.DaemonJob{}, "id in (?)", jobIDs)

	if ret.Error != nil {
		return ret.Error
	}

	return nil
}

func (j *DaemonJob) Get(jobID uint, djob *models.DaemonJob) error {
	return models.DB().Find(djob, "id=?", jobID).Error
}

func (j *DaemonJob) Log(args proto.SearchLog, reply *proto.SearchLogResult) error {

	fd := finder.NewFinder(1000000, func(info os.FileInfo) bool {
		basename := filepath.Base(info.Name())
		arr := strings.Split(basename, ".")
		if len(arr) != 2 {
			return false
		}
		if arr[1] == "log" && arr[0] == fmt.Sprint(args.JobID) {
			return true
		}
		return false
	})

	if args.Date == "" {
		args.Date = time.Now().Format("2006/01/02")
	}

	if args.IsTail {
		fd.SetTail(true)
	}

	rootpath := filepath.Join(cfg.LogPath, "daemon_job", args.Date)
	err := fd.Search(rootpath, args.Pattern, &reply.Content, args.Page, args.Pagesize)
	reply.Total = int(fd.Count())
	return err

}

func (j *DaemonJob) Audit(args proto.AuditJobArgs, reply *bool) error {
	*reply = true
	return models.DB().Model(&models.DaemonJob{}).Where("id in (?) and status=?", args.JobIDs, models.StatusJobUnaudited).Update("status", models.StatusJobOk).Error
}