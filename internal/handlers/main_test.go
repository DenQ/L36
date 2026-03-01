package handlers

import (
	"testing"
)

func TestAllHandlers(t *testing.T) {
	// Сначала проверяем базу (последовательно)
	t.Run("TestFullPageFlow", TestFullPageFlow)
	t.Run("TestDeletePage", TestDeletePage)
	t.Run("TestConcurrentVersions", TestConcurrentVersions)
	t.Run("TestSetLatestRollback", TestSetLatestRollback)
	t.Run("E2E_Logic", TestE2EPageFlow)

	// В самом конце даем жару стресс-тестом
	t.Run("Stress", TestE2ELoadStress)
}
