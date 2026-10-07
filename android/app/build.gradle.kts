plugins { id("com.android.application"); id("org.jetbrains.kotlin.android"); id("org.jetbrains.kotlin.kapt") }
android {
 namespace = "com.qianben.app"
 compileSdk = 35
 defaultConfig { applicationId = "com.qianben.app"; minSdk = 26; targetSdk = 35; versionCode = 3; versionName = "0.1.2";testInstrumentationRunner="androidx.test.runner.AndroidJUnitRunner" }
 buildFeatures { buildConfig = true }
 compileOptions { sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
 kotlinOptions { jvmTarget = "17" }
}
dependencies {
 implementation("androidx.room:room-runtime:2.7.2")
 kapt("androidx.room:room-compiler:2.7.2")
 implementation("androidx.work:work-runtime:2.10.1")
 androidTestImplementation("androidx.test:runner:1.6.2")
 androidTestImplementation("androidx.test.ext:junit:1.2.1")
}
