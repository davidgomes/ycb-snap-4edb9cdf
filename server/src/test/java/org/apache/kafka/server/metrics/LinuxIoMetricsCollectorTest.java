/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements. See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License. You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package org.apache.kafka.server.metrics;

import org.apache.kafka.common.utils.MockTime;
import org.apache.kafka.common.utils.Time;
import org.apache.kafka.test.TestUtils;

import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;

import java.io.File;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

@Timeout(120)
public class LinuxIoMetricsCollectorTest {

    @Test
    public void testReadProcFile() throws IOException {
        TestDirectory testDirectory = new TestDirectory();
        Time time = new MockTime(0L, 100L, 1000L);
        testDirectory.writeProcFile(1L, 2L, 3L, 4L, 123L, 456L, 5L);
        LinuxIoMetricsCollector collector = new LinuxIoMetricsCollector(testDirectory.baseDir.getAbsolutePath(), time);

        // Test that we can read the values we wrote.
        assertTrue(collector.usable());
        assertEquals(1L, collector.rchar());
        assertEquals(2L, collector.wchar());
        assertEquals(3L, collector.syscr());
        assertEquals(4L, collector.syscw());
        assertEquals(123L, collector.readBytes());
        assertEquals(456L, collector.writeBytes());
        assertEquals(5L, collector.cancelledWriteBytes());
        testDirectory.writeProcFile(11L, 22L, 33L, 44L, 124L, 457L, 55L);

        // The previous values should still be cached.
        assertEquals(1L, collector.rchar());
        assertEquals(2L, collector.wchar());
        assertEquals(3L, collector.syscr());
        assertEquals(4L, collector.syscw());
        assertEquals(123L, collector.readBytes());
        assertEquals(456L, collector.writeBytes());
        assertEquals(5L, collector.cancelledWriteBytes());

        // Update the time, and the values should be re-read.
        time.sleep(1);
        assertEquals(11L, collector.rchar());
        assertEquals(22L, collector.wchar());
        assertEquals(33L, collector.syscr());
        assertEquals(44L, collector.syscw());
        assertEquals(124L, collector.readBytes());
        assertEquals(457L, collector.writeBytes());
        assertEquals(55L, collector.cancelledWriteBytes());
    }

    @Test
    public void testUnableToReadNonexistentProcFile() throws IOException {
        TestDirectory testDirectory = new TestDirectory();
        Time time = new MockTime(0L, 100L, 1000L);
        LinuxIoMetricsCollector collector = new LinuxIoMetricsCollector(testDirectory.baseDir.getAbsolutePath(), time);

        // Test that we can't read the file, since it hasn't been written.
        assertFalse(collector.usable());
    }

    static class TestDirectory {

        public final File baseDir;
        private final Path selfDir;

        TestDirectory() throws IOException {
            baseDir = TestUtils.tempDirectory();
            selfDir = Files.createDirectories(baseDir.toPath().resolve("self"));
        }

        void writeProcFile(
            long rchar,
            long wchar,
            long syscr,
            long syscw,
            long readBytes,
            long writeBytes,
            long cancelledWriteBytes
        ) throws IOException {
            String bld = "rchar: " + rchar + "\n" +
                         "wchar: " + wchar + "\n" +
                         "syscr: " + syscr + "\n" +
                         "syscw: " + syscw + "\n" +
                         "read_bytes: " + readBytes + "\n" +
                         "write_bytes: " + writeBytes + "\n" +
                         "cancelled_write_bytes: " + cancelledWriteBytes + "\n";
            Files.writeString(selfDir.resolve("io"), bld);
        }
    }
}
