import Foundation
#if canImport(LocalAuthentication)
import LocalAuthentication

func makeAuthenticator() throws -> any Authenticator { TouchIDAuthenticator() }

final class TouchIDAuthenticator: Authenticator {
    func available() -> Bool {
        let context = LAContext()
        var error: NSError?
        guard context.canEvaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, error: &error) else {
            return false
        }
        return context.biometryType == .touchID
    }

    func authenticate(reason: String) async throws {
        let context = LAContext()
        context.localizedFallbackTitle = "Use PIN"
        do {
            try await withTaskCancellationHandler {
                _ = try await context.evaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, localizedReason: reason)
            } onCancel: {
                context.invalidate()
            }
        } catch let error as LAError {
            switch error.code {
            case .userCancel, .userFallback, .systemCancel, .appCancel:
                throw TouchidError.canceled
            case .authenticationFailed, .biometryLockout:
                throw TouchidError.failed
            default:
                throw error
            }
        }
    }
}
#else
func makeAuthenticator() throws -> any Authenticator { throw NativeError.unsupported }
#endif
