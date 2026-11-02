package evolver

// trustedSensorFixture documents the numbers the system verifier compares.
// The candidate process never receives this document, and a generated
// expected.json is not consulted.
func trustedSensorFixture() map[string]any {
	return sensorLogContract().Expect
}
