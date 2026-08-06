package domain

func ValidProjectPlanTransition(from, to string) bool {
	return (from == "draft" && to == "active") || (from == "active" && to == "closed")
}
