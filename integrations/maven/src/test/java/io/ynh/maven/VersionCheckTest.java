package io.ynh.maven;

import io.ynh.maven.util.VersionCheck;
import org.junit.Test;

import static org.junit.Assert.*;

public class VersionCheckTest {

    @Test
    public void equalVersionsPasses() {
        assertTrue(VersionCheck.meetsMinimum("0.1.0", "0.1.0"));
    }

    @Test
    public void higherMajorPasses() {
        assertTrue(VersionCheck.meetsMinimum("1.0.0", "0.1.0"));
    }

    @Test
    public void higherMinorPasses() {
        assertTrue(VersionCheck.meetsMinimum("0.2.0", "0.1.0"));
    }

    @Test
    public void higherPatchPasses() {
        assertTrue(VersionCheck.meetsMinimum("0.1.1", "0.1.0"));
    }

    @Test
    public void lowerMajorFails() {
        assertFalse(VersionCheck.meetsMinimum("0.0.9", "0.1.0"));
    }

    @Test
    public void lowerMinorFails() {
        assertFalse(VersionCheck.meetsMinimum("0.0.5", "0.1.0"));
    }

    @Test
    public void handlesMissingPatch() {
        assertTrue(VersionCheck.meetsMinimum("1.0", "0.1.0"));
    }

    @Test
    public void handlesNonNumericGracefully() {
        // Should not throw — treats non-numeric as 0
        assertTrue(VersionCheck.meetsMinimum("1.0.beta", "0.1.0"));
    }
}
