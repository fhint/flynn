// +build linux

package devicemapper

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"syscall"

	log "github.com/flynn/flynn/Godeps/_workspace/src/github.com/Sirupsen/logrus"
)

type DevmapperLogger interface {
	DMLog(level int, file string, line int, dmError int, message string)
}

const (
	DeviceCreate TaskType = iota
	DeviceReload
	DeviceRemove
	DeviceRemoveAll
	DeviceSuspend
	DeviceResume
	DeviceInfo
	DeviceDeps
	DeviceRename
	DeviceVersion
	DeviceStatus
	DeviceTable
	DeviceWaitevent
	DeviceList
	DeviceClear
	DeviceMknodes
	DeviceListVersions
	DeviceTargetMsg
	DeviceSetGeometry
)

const (
	AddNodeOnResume AddNodeType = iota
	AddNodeOnCreate
)

var (
	ErrTaskRun                = errors.New("dm_task_run failed")
	ErrTaskSetName            = errors.New("dm_task_set_name failed")
	ErrTaskSetMessage         = errors.New("dm_task_set_message failed")
	ErrTaskSetAddNode         = errors.New("dm_task_set_add_node failed")
	ErrTaskSetRo              = errors.New("dm_task_set_ro failed")
	ErrTaskAddTarget          = errors.New("dm_task_add_target failed")
	ErrTaskSetSector          = errors.New("dm_task_set_sector failed")
	ErrTaskGetDeps            = errors.New("dm_task_get_deps failed")
	ErrTaskGetInfo            = errors.New("dm_task_get_info failed")
	ErrTaskGetDriverVersion   = errors.New("dm_task_get_driver_version failed")
	ErrTaskSetCookie          = errors.New("dm_task_set_cookie failed")
	ErrNilCookie              = errors.New("cookie ptr can't be nil")
	ErrAttachLoopbackDevice   = errors.New("loopback mounting failed")
	ErrGetBlockSize           = errors.New("Can't get block size")
	ErrUdevWait               = errors.New("wait on udev cookie failed")
	ErrSetDevDir              = errors.New("dm_set_dev_dir failed")
	ErrGetLibraryVersion      = errors.New("dm_get_library_version failed")
	ErrCreateRemoveTask       = errors.New("Can't create task of type DeviceRemove")
	ErrRunRemoveDevice        = errors.New("running RemoveDevice failed")
	ErrInvalidAddNode         = errors.New("Invalid AddNode type")
	ErrGetLoopbackBackingFile = errors.New("Unable to get loopback backing file")
	ErrLoopbackSetCapacity    = errors.New("Unable set loopback capacity")
	ErrBusy                   = errors.New("Device is Busy")

	dmSawBusy  bool
	dmSawExist bool
)

type (
	Task struct {
		unmanaged *CDmTask
	}
	Deps struct {
		Count  uint32
		Filler uint32
		Device []uint64
	}
	Info struct {
		Exists        int
		Suspended     int
		LiveTable     int
		InactiveTable int
		OpenCount     int32
		EventNr       uint32
		Major         uint32
		Minor         uint32
		ReadOnly      int
		TargetCount   int32
	}
	TaskType    int
	AddNodeType int
)

func (t *Task) destroy() {
	if t != nil {
		DmTaskDestroy(t.unmanaged)
		runtime.SetFinalizer(t, nil)
	}
}

// TaskCreateNamed is a convenience function for TaskCreate when a name
// will be set on the task as well
func TaskCreateNamed(t TaskType, name string) (*Task, error) {
	task := TaskCreate(t)
	if task == nil {
		return nil, fmt.Errorf("Can't create task of type %d", int(t))
	}
	if err := task.SetName(name); err != nil {
		return nil, fmt.Errorf("Can't set task name %s", name)
	}
	return task, nil
}

// TaskCreate initializes a devicemapper task of tasktype
func TaskCreate(tasktype TaskType) *Task {
	Ctask := DmTaskCreate(int(tasktype))
	if Ctask == nil {
		return nil
	}
	task := &Task{unmanaged: Ctask}
	runtime.SetFinalizer(task, (*Task).destroy)
	return task
}

func (t *Task) Run() error {
	if res := DmTaskRun(t.unmanaged); res != 1 {
		return ErrTaskRun
	}
	return nil
}

func (t *Task) SetName(name string) error {
	if res := DmTaskSetName(t.unmanaged, name); res != 1 {
		return ErrTaskSetName
	}
	return nil
}

func (t *Task) SetMessage(message string) error {
	if res := DmTaskSetMessage(t.unmanaged, message); res != 1 {
		return ErrTaskSetMessage
	}
	return nil
}

func (t *Task) SetSector(sector uint64) error {
	if res := DmTaskSetSector(t.unmanaged, sector); res != 1 {
		return ErrTaskSetSector
	}
	return nil
}

func (t *Task) SetCookie(cookie *uint, flags uint16) error {
	if cookie == nil {
		return ErrNilCookie
	}
	if res := DmTaskSetCookie(t.unmanaged, cookie, flags); res != 1 {
		return ErrTaskSetCookie
	}
	return nil
}

func (t *Task) SetAddNode(addNode AddNodeType) error {
	if addNode != AddNodeOnResume && addNode != AddNodeOnCreate {
		return ErrInvalidAddNode
	}
	if res := DmTaskSetAddNode(t.unmanaged, addNode); res != 1 {
		return ErrTaskSetAddNode
	}
	return nil
}

func (t *Task) SetRo() error {
	if res := DmTaskSetRo(t.unmanaged); res != 1 {
		return ErrTaskSetRo
	}
	return nil
}

func (t *Task) AddTarget(start, size uint64, ttype, params string) error {
	if res := DmTaskAddTarget(t.unmanaged, start, size,
		ttype, params); res != 1 {
		return ErrTaskAddTarget
	}
	return nil
}

func (t *Task) GetDeps() (*Deps, error) {
	var deps *Deps
	if deps = DmTaskGetDeps(t.unmanaged); deps == nil {
		return nil, ErrTaskGetDeps
	}
	return deps, nil
}

func (t *Task) GetInfo() (*Info, error) {
	info := &Info{}
	if res := DmTaskGetInfo(t.unmanaged, info); res != 1 {
		return nil, ErrTaskGetInfo
	}
	return info, nil
}

func (t *Task) GetDriverVersion() (string, error) {
	res := DmTaskGetDriverVersion(t.unmanaged)
	if res == "" {
		return "", ErrTaskGetDriverVersion
	}
	return res, nil
}

func (t *Task) GetNextTarget(next uintptr) (nextPtr uintptr, start uint64,
	length uint64, targetType string, params string) {

	return DmGetNextTarget(t.unmanaged, next, &start, &length,
			&targetType, &params),
		start, length, targetType, params
}

func getLoopbackBackingFile(file *os.File) (uint64, uint64, error) {
	loopInfo, err := ioctlLoopGetStatus64(file.Fd())
	if err != nil {
		log.Errorf("Error get loopback backing file: %s", err)
		return 0, 0, ErrGetLoopbackBackingFile
	}
	return loopInfo.loDevice, loopInfo.loInode, nil
}

func LoopbackSetCapacity(file *os.File) error {
	if err := ioctlLoopSetCapacity(file.Fd(), 0); err != nil {
		log.Errorf("Error loopbackSetCapacity: %s", err)
		return ErrLoopbackSetCapacity
	}
	return nil
}

func FindLoopDeviceFor(file *os.File) *os.File {
	stat, err := file.Stat()
	if err != nil {
		return nil
	}
	targetInode := stat.Sys().(*syscall.Stat_t).Ino
	targetDevice := stat.Sys().(*syscall.Stat_t).Dev

	for i := 0; true; i++ {
		path := fmt.Sprintf("/dev/loop%d", i)

		file, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}

			// Ignore all errors until the first not-exist
			// we want to continue looking for the file
			continue
		}

		dev, inode, err := getLoopbackBackingFile(file)
		if err == nil && dev == targetDevice && inode == targetInode {
			return file
		}
		file.Close()
	}

	return nil
}

func UdevWait(cookie uint) error {
	if res := DmUdevWait(cookie); res != 1 {
		log.Debugf("Failed to wait on udev cookie %d", cookie)
		return ErrUdevWait
	}
	return nil
}

func LogInitVerbose(level int) {
	DmLogInitVerbose(level)
}

var dmLogger DevmapperLogger = nil

// initialize the logger for the device mapper library
func LogInit(logger DevmapperLogger) {
	dmLogger = logger
	LogWithErrnoInit()
}

func SetDevDir(dir string) error {
	if res := DmSetDevDir(dir); res != 1 {
		log.Debugf("Error dm_set_dev_dir")
		return ErrSetDevDir
	}
	return nil
}

func GetLibraryVersion() (string, error) {
	var version string
	if res := DmGetLibraryVersion(&version); res != 1 {
		return "", ErrGetLibraryVersion
	}
	return version, nil
}

// Useful helper for cleanup
func RemoveDevice(name string) error {
	log.Debugf("[devmapper] RemoveDevice START")
	defer log.Debugf("[devmapper] RemoveDevice END")
	task, err := TaskCreateNamed(DeviceRemove, name)
	if task == nil {
		return err
	}

	var cookie uint = 0
	if err := task.SetCookie(&cookie, 0); err != nil {
		return fmt.Errorf("Can not set cookie: %s", err)
	}
	defer UdevWait(cookie)

	dmSawBusy = false // reset before the task is run
	if err = task.Run(); err != nil {
		if dmSawBusy {
			return ErrBusy
		}
		return fmt.Errorf("Error running RemoveDevice %s", err)
	}

	return nil
}

func GetBlockDeviceSize(file *os.File) (uint64, error) {
	size, err := ioctlBlkGetSize64(file.Fd())
	if err != nil {
		log.Errorf("Error getblockdevicesize: %s", err)
		return 0, ErrGetBlockSize
	}
	return uint64(size), nil
}

func BlockDeviceDiscard(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer file.Close()

	size, err := GetBlockDeviceSize(file)
	if err != nil {
		return err
	}

	if err := ioctlBlkDiscard(file.Fd(), 0, size); err != nil {
		return err
	}

	// Without this sometimes the remove of the device that happens after
	// discard fails with EBUSY.
	syscall.Sync()

	return nil
}

// This is the programmatic example of "dmsetup create"
func CreatePool(poolName string, dataFile, metadataFile *os.File, poolBlockSize uint32) error {
	task, err := TaskCreateNamed(DeviceCreate, poolName)
	if task == nil {
		return err
	}

	size, err := GetBlockDeviceSize(dataFile)
	if err != nil {
		return fmt.Errorf("Can't get data size %s", err)
	}

	params := fmt.Sprintf("%s %s %d 32768 1 skip_block_zeroing", metadataFile.Name(), dataFile.Name(), poolBlockSize)
	if err := task.AddTarget(0, size/512, "thin-pool", params); err != nil {
		return fmt.Errorf("Can't add target %s", err)
	}

	var cookie uint = 0
	var flags uint16 = DmUdevDisableSubsystemRulesFlag | DmUdevDisableDiskRulesFlag | DmUdevDisableOtherRulesFlag
	if err := task.SetCookie(&cookie, flags); err != nil {
		return fmt.Errorf("Can't set cookie %s", err)
	}
	defer UdevWait(cookie)

	if err := task.Run(); err != nil {
		return fmt.Errorf("Error running DeviceCreate (CreatePool) %s", err)
	}

	return nil
}

func ReloadPool(poolName string, dataFile, metadataFile *os.File, poolBlockSize uint32) error {
	task, err := TaskCreateNamed(DeviceReload, poolName)
	if task == nil {
		return err
	}

	size, err := GetBlockDeviceSize(dataFile)
	if err != nil {
		return fmt.Errorf("Can't get data size %s", err)
	}

	params := fmt.Sprintf("%s %s %d 32768 1 skip_block_zeroing", metadataFile.Name(), dataFile.Name(), poolBlockSize)
	if err := task.AddTarget(0, size/512, "thin-pool", params); err != nil {
		return fmt.Errorf("Can't add target %s", err)
	}

	if err := task.Run(); err != nil {
		return fmt.Errorf("Error running DeviceCreate %s", err)
	}

	return nil
}

func GetDeps(name string) (*Deps, error) {
	task, err := TaskCreateNamed(DeviceDeps, name)
	if task == nil {
		return nil, err
	}
	if err := task.Run(); err != nil {
		return nil, err
	}
	return task.GetDeps()
}

func GetInfo(name string) (*Info, error) {
	task, err := TaskCreateNamed(DeviceInfo, name)
	if task == nil {
		return nil, err
	}
	if err := task.Run(); err != nil {
		return nil, err
	}
	return task.GetInfo()
}

func GetDriverVersion() (string, error) {
	task := TaskCreate(DeviceVersion)
	if task == nil {
		return "", fmt.Errorf("Can't create DeviceVersion task")
	}
	if err := task.Run(); err != nil {
		return "", err
	}
	return task.GetDriverVersion()
}

func GetStatus(name string) (uint64, uint64, string, string, error) {
	task, err := TaskCreateNamed(DeviceStatus, name)
	if task == nil {
		log.Debugf("GetStatus: Error TaskCreateNamed: %s", err)
		return 0, 0, "", "", err
	}
	if err := task.Run(); err != nil {
		log.Debugf("GetStatus: Error Run: %s", err)
		return 0, 0, "", "", err
	}

	devinfo, err := task.GetInfo()
	if err != nil {
		log.Debugf("GetStatus: Error GetInfo: %s", err)
		return 0, 0, "", "", err
	}
	if devinfo.Exists == 0 {
		log.Debugf("GetStatus: Non existing device %s", name)
		return 0, 0, "", "", fmt.Errorf("Non existing device %s", name)
	}

	_, start, length, targetType, params := task.GetNextTarget(0)
	return start, length, targetType, params, nil
}

func SetTransactionId(poolName string, oldId uint64, newId uint64) error {
	task, err := TaskCreateNamed(DeviceTargetMsg, poolName)
	if task == nil {
		return err
	}

	if err := task.SetSector(0); err != nil {
		return fmt.Errorf("Can't set sector %s", err)
	}

	if err := task.SetMessage(fmt.Sprintf("set_transaction_id %d %d", oldId, newId)); err != nil {
		return fmt.Errorf("Can't set message %s", err)
	}

	if err := task.Run(); err != nil {
		return fmt.Errorf("Error running SetTransactionId %s", err)
	}
	return nil
}

func SuspendDevice(name string) error {
	task, err := TaskCreateNamed(DeviceSuspend, name)
	if task == nil {
		return err
	}
	if err := task.Run(); err != nil {
		return fmt.Errorf("Error running DeviceSuspend %s", err)
	}
	return nil
}

func ResumeDevice(name string) error {
	task, err := TaskCreateNamed(DeviceResume, name)
	if task == nil {
		return err
	}

	var cookie uint = 0
	if err := task.SetCookie(&cookie, 0); err != nil {
		return fmt.Errorf("Can't set cookie %s", err)
	}
	defer UdevWait(cookie)

	if err := task.Run(); err != nil {
		return fmt.Errorf("Error running DeviceResume %s", err)
	}

	return nil
}

func CreateDevice(poolName string, deviceId *int) error {
	log.Debugf("[devmapper] CreateDevice(poolName=%v, deviceId=%v)", poolName, *deviceId)

	for {
		task, err := TaskCreateNamed(DeviceTargetMsg, poolName)
		if task == nil {
			return err
		}

		if err := task.SetSector(0); err != nil {
			return fmt.Errorf("Can't set sector %s", err)
		}

		if err := task.SetMessage(fmt.Sprintf("create_thin %d", *deviceId)); err != nil {
			return fmt.Errorf("Can't set message %s", err)
		}

		dmSawExist = false // reset before the task is run
		if err := task.Run(); err != nil {
			if dmSawExist {
				// Already exists, try next id
				*deviceId++
				continue
			}
			return fmt.Errorf("Error running CreateDevice %s", err)
		}
		break
	}
	return nil
}

func DeleteDevice(poolName string, deviceId int) error {
	task, err := TaskCreateNamed(DeviceTargetMsg, poolName)
	if task == nil {
		return err
	}

	if err := task.SetSector(0); err != nil {
		return fmt.Errorf("Can't set sector %s", err)
	}

	if err := task.SetMessage(fmt.Sprintf("delete %d", deviceId)); err != nil {
		return fmt.Errorf("Can't set message %s", err)
	}

	if err := task.Run(); err != nil {
		return fmt.Errorf("Error running DeleteDevice %s", err)
	}
	return nil
}

func ActivateDevice(poolName string, name string, deviceId int, size uint64) error {
	task, err := TaskCreateNamed(DeviceCreate, name)
	if task == nil {
		return err
	}

	params := fmt.Sprintf("%s %d", poolName, deviceId)
	if err := task.AddTarget(0, size/512, "thin", params); err != nil {
		return fmt.Errorf("Can't add target %s", err)
	}
	if err := task.SetAddNode(AddNodeOnCreate); err != nil {
		return fmt.Errorf("Can't add node %s", err)
	}

	var cookie uint = 0
	if err := task.SetCookie(&cookie, 0); err != nil {
		return fmt.Errorf("Can't set cookie %s", err)
	}

	defer UdevWait(cookie)

	if err := task.Run(); err != nil {
		return fmt.Errorf("Error running DeviceCreate (ActivateDevice) %s", err)
	}

	return nil
}

func CreateSnapDevice(poolName string, deviceId *int, baseName string, baseDeviceId int) error {
	devinfo, _ := GetInfo(baseName)
	doSuspend := devinfo != nil && devinfo.Exists != 0

	if doSuspend {
		if err := SuspendDevice(baseName); err != nil {
			return err
		}
	}

	for {
		task, err := TaskCreateNamed(DeviceTargetMsg, poolName)
		if task == nil {
			if doSuspend {
				ResumeDevice(baseName)
			}
			return err
		}

		if err := task.SetSector(0); err != nil {
			if doSuspend {
				ResumeDevice(baseName)
			}
			return fmt.Errorf("Can't set sector %s", err)
		}

		if err := task.SetMessage(fmt.Sprintf("create_snap %d %d", *deviceId, baseDeviceId)); err != nil {
			if doSuspend {
				ResumeDevice(baseName)
			}
			return fmt.Errorf("Can't set message %s", err)
		}

		dmSawExist = false // reset before the task is run
		if err := task.Run(); err != nil {
			if dmSawExist {
				// Already exists, try next id
				*deviceId++
				continue
			}

			if doSuspend {
				ResumeDevice(baseName)
			}
			return fmt.Errorf("Error running DeviceCreate (createSnapDevice) %s", err)
		}

		break
	}

	if doSuspend {
		if err := ResumeDevice(baseName); err != nil {
			return err
		}
	}

	return nil
}
