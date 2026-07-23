"""VastPlan first-party Python plugin SDK."""

from .plugin import Contribution, InvocationContext, Plugin
from .context import ContextViews
from .credential import ManagedCredentialRef, managed_credential_refs
from .scoped_configuration import RevisionObservation, ScopedConfigurationClient, ScopedResolution
from .shared_state import SharedStateClient, SharedStateEntry, SharedStateError, SharedStatePage, is_shared_state_conflict, is_shared_state_not_found

__all__ = [
    "ContextViews", "Contribution", "InvocationContext", "ManagedCredentialRef", "Plugin",
    "RevisionObservation", "ScopedConfigurationClient", "ScopedResolution", "managed_credential_refs",
    "SharedStateClient", "SharedStateEntry", "SharedStateError", "SharedStatePage",
    "is_shared_state_conflict", "is_shared_state_not_found",
]
