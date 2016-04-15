package rancher

type Service struct {
	Id            string `json:"id"`
	Name          string `json:"name"`
	Scale         int64  `json:"scale"`
	Transitioning string `json:"transitioning"`
}
