package phoneupload

import "time"

func (service *Service) SetClock(now func() time.Time) {
	service.now = now
}
