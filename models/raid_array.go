package models

type RaidArray struct {
	ID         string `gorm:"primaryKey" json:"id"`
	Name       string `json:"name"`
	RaidLevel  string `json:"raid_level"`
	RaidName   string `json:"raid_name"`
	DevicePath string `json:"device_path"`
	Status     string `json:"status"`
	Disk1      string `json:"disk1"`
	Disk2      string `json:"disk2"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

func (RaidArray) TableName() string {
	return "raid_arrays"
}
