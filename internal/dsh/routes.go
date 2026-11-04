package dsh

import "github.com/go-chi/chi/v5"

func ConfigureRouter(router chi.Router) {
	router.NotFound(configureNotFound)
	router.MethodNotAllowed(configureMethodNotAllowed)
}
