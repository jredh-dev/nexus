import SwiftUI

@main
struct ShenApp: App {
    @StateObject private var store = SecretsStore()

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(store)
                .frame(minWidth: 600, minHeight: 400)
        }
        .windowStyle(.titleBar)
        .defaultSize(width: 800, height: 600)
    }
}
