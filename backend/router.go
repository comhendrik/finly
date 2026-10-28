package backend

import (
	"fmt"
	"html/template"
	"strings"

	"github.com/gin-gonic/gin"
)

func (a *App) router() *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery(), a.authMiddleware())
	router.SetFuncMap(template.FuncMap{
		"money": func(value float64) string {
			return fmt.Sprintf("%.2f", value)
		},
		"percent": func(spent, budget float64) int {
			if budget <= 0 {
				return 0
			}
			value := int(spent / budget * 100)
			if value > 100 {
				return 100
			}
			return value
		},
		"add": func(left, right float64) float64 {
			return left + right
		},
		"join": strings.Join,
	})

	router.LoadHTMLGlob("frontend/templates/*")
	router.Static("/static", "frontend/static")
	router.GET("/health", a.health)
	router.GET("/login", a.loginPage)
	router.POST("/login", a.login)
	router.GET("/signup", a.signupPage)
	router.POST("/signup", a.signup)
	router.GET("/setup", a.setupPage)
	router.POST("/setup", a.setup)
	router.POST("/logout", a.logout)
	router.POST("/settings/api-key", a.createAPIKey)
	router.POST("/settings/api-key/revoke", a.revokeAPIKey)
	router.POST("/account/password", a.changePassword)
	router.POST("/account/delete", a.deleteAccount)
	router.GET("/account/export", a.exportAccountData)
	router.POST("/account/import", a.importAccountData)
	router.POST("/api/v1/expenses", a.apiAuthMiddleware(), a.apiCreateExpense)

	router.GET("/", a.home)
	router.POST("/month", a.changeMonth)
	router.POST("/settings", a.saveSettings)
	router.POST("/settings/active-month", a.setActiveMonth)
	router.POST("/settings/month/delete", a.deleteMonthData)

	router.POST("/budget/:id/expense", a.addBudgetExpense)
	router.POST("/budget/:id/expense/:expenseID/delete", a.deleteBudgetExpense)
	router.POST("/standard-expenses", a.addStandardExpense)
	router.POST("/standard-expenses/:id/delete", a.deleteStandardExpense)
	router.POST("/standard-expenses/:id/category", a.updateExpenseCategory)

	return router
}
