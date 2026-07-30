use axum::http::{header, StatusCode};
use axum::response::{IntoResponse, Response};
use axum::Json;
use serde_json::json;

use crate::engine::EngineError;
use crate::worker::WorkerError;

#[derive(Debug, Clone, thiserror::Error)]
pub enum AppError {
    #[error("image not found")]
    NotFound,
    #[error("{0}")]
    Forbidden(String),
    #[error("{0}")]
    BadRequest(String),
    #[error("service overloaded")]
    Overloaded,
    #[error("source image too large")]
    TooLarge,
    #[error("upstream fetch failed")]
    Upstream,
    #[error("image processing failed")]
    Internal,
}

impl AppError {
    fn status(&self) -> StatusCode {
        match self {
            AppError::NotFound => StatusCode::NOT_FOUND,
            AppError::Forbidden(_) => StatusCode::FORBIDDEN,
            AppError::BadRequest(_) => StatusCode::BAD_REQUEST,
            AppError::Overloaded => StatusCode::SERVICE_UNAVAILABLE,
            AppError::TooLarge => StatusCode::PAYLOAD_TOO_LARGE,
            AppError::Upstream => StatusCode::BAD_GATEWAY,
            AppError::Internal => StatusCode::INTERNAL_SERVER_ERROR,
        }
    }
}

impl IntoResponse for AppError {
    fn into_response(self) -> Response {
        let body = Json(json!({ "error": self.to_string() }));
        (
            self.status(),
            [
                (header::CACHE_CONTROL, "no-store"),
                (header::ACCESS_CONTROL_ALLOW_ORIGIN, "*"),
            ],
            body,
        )
            .into_response()
    }
}

impl From<EngineError> for AppError {
    fn from(value: EngineError) -> Self {
        match value {
            EngineError::SourceTooLarge { .. } => AppError::TooLarge,
            EngineError::Decode => AppError::BadRequest("unsupported or corrupt image".to_string()),
            EngineError::Init | EngineError::Vips(_) => AppError::Internal,
        }
    }
}

impl From<WorkerError> for AppError {
    fn from(value: WorkerError) -> Self {
        match value {
            WorkerError::Overloaded => AppError::Overloaded,
            WorkerError::Closed | WorkerError::Panicked => AppError::Internal,
        }
    }
}

impl From<object_store::Error> for AppError {
    fn from(value: object_store::Error) -> Self {
        match value {
            object_store::Error::NotFound { .. } => AppError::NotFound,
            _ => AppError::Upstream,
        }
    }
}
