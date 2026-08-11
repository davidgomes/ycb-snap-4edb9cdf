/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */
package org.apache.pulsar.broker.service;

import static java.util.Collections.emptyMap;
import static org.apache.pulsar.client.api.MessageId.latest;
import static org.apache.pulsar.common.api.proto.CommandSubscribe.SubType.Exclusive;
import static org.apache.pulsar.common.api.proto.CommandSubscribe.SubType.Shared;
import static org.apache.pulsar.common.api.proto.KeySharedMode.AUTO_SPLIT;
import static org.apache.pulsar.common.protocol.Commands.DEFAULT_CONSUMER_EPOCH;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.when;
import static org.testng.Assert.assertEquals;
import static org.testng.Assert.assertFalse;
import static org.testng.Assert.assertTrue;
import java.net.SocketAddress;
import java.util.BitSet;
import org.apache.bookkeeper.mledger.ManagedCursor;
import org.apache.bookkeeper.mledger.Position;
import org.apache.pulsar.broker.PulsarService;
import org.apache.pulsar.broker.ServiceConfiguration;
import org.apache.pulsar.broker.service.persistent.PersistentSubscription;
import org.apache.pulsar.broker.service.persistent.PersistentTopic;
import org.apache.pulsar.common.api.proto.KeySharedMeta;
import org.apache.pulsar.common.policies.data.HierarchyTopicPolicies;
import org.apache.pulsar.common.policies.data.stats.ConsumerStatsImpl;
import org.testng.annotations.BeforeMethod;
import org.testng.annotations.Test;

@Test(groups = "broker")
public class ConsumerTest {
    private Consumer consumer;
    private final ConsumerStatsImpl stats = new ConsumerStatsImpl();

    @BeforeMethod
    public void beforeMethod() {
        Subscription subscription = mock(Subscription.class);
        ServerCnx cnx = mock(ServerCnx.class);
        SocketAddress address = mock(SocketAddress.class);
        Topic topic = mock(Topic.class);
        BrokerService brokerService = mock(BrokerService.class);
        PulsarService pulsarService = mock(PulsarService.class);
        ServiceConfiguration serviceConfiguration = mock(ServiceConfiguration.class);

        when(cnx.clientAddress()).thenReturn(address);
        when(subscription.getTopic()).thenReturn(topic);
        when(topic.getBrokerService()).thenReturn(brokerService);
        when(brokerService.getPulsar()).thenReturn(pulsarService);
        when(pulsarService.getConfiguration()).thenReturn(serviceConfiguration);

        consumer =
                new Consumer(subscription, Exclusive, "topic", 1, 0, "Cons1", true, cnx, "myrole-1", emptyMap(), false,
                        new KeySharedMeta().setKeySharedMode(AUTO_SPLIT), latest, DEFAULT_CONSUMER_EPOCH);
    }

    @Test
    public void testGetMsgOutCounter() {
        stats.msgOutCounter = 1L;
        consumer.updateStats(stats);
        assertEquals(consumer.getMsgOutCounter(), 1L);
    }

    @Test
    public void testGetBytesOutCounter() {
        stats.bytesOutCounter = 1L;
        consumer.updateStats(stats);
        assertEquals(consumer.getBytesOutCounter(), 1L);
    }

    @Test
    public void testRemovePendingAcksUpToPositionDecrementsUnackedForNonBatchAndFullBatch() {
        PersistentSubscription subscription = mock(PersistentSubscription.class);
        Consumer sharedConsumer = newSharedPersistentConsumer(subscription, false);

        sharedConsumer.getPendingAcks().addPendingAckIfAllowed(1L, 1L, 1, 100);
        sharedConsumer.getPendingAcks().addPendingAckIfAllowed(1L, 2L, 5, 101);
        sharedConsumer.getPendingAcks().addPendingAckIfAllowed(1L, 3L, 1, 102);

        ConsumerStatsImpl consumerStats = new ConsumerStatsImpl();
        consumerStats.unackedMessages = 7;
        sharedConsumer.updateStats(consumerStats);
        assertEquals(sharedConsumer.getUnackedMessages(), 7);

        sharedConsumer.removePendingAcksUpToPositionAndDecrementUnacked(1L, 2L);

        assertEquals(sharedConsumer.getUnackedMessages(), 1);
        assertFalse(sharedConsumer.getPendingAcks().contains(1L, 1L));
        assertFalse(sharedConsumer.getPendingAcks().contains(1L, 2L));
        assertTrue(sharedConsumer.getPendingAcks().contains(1L, 3L));
    }

    @Test
    public void testRemovePendingAcksUpToPositionDecrementsOnlyUnackedBatchIndexes() {
        PersistentSubscription subscription = mock(PersistentSubscription.class);
        ManagedCursor cursor = mock(ManagedCursor.class);
        when(subscription.getCursor()).thenReturn(cursor);

        // 10-message batch with indexes 0-4 already acked; 5 still-unacked bits remain.
        BitSet remainingUnacked = new BitSet(10);
        remainingUnacked.set(5, 10);
        when(cursor.getDeletedBatchIndexesAsLongArray(any(Position.class)))
                .thenReturn(remainingUnacked.toLongArray());

        Consumer sharedConsumer = newSharedPersistentConsumer(subscription, true);
        sharedConsumer.getPendingAcks().addPendingAckIfAllowed(1L, 1L, 10, 100);

        ConsumerStatsImpl consumerStats = new ConsumerStatsImpl();
        consumerStats.unackedMessages = 5;
        sharedConsumer.updateStats(consumerStats);
        assertEquals(sharedConsumer.getUnackedMessages(), 5);

        sharedConsumer.removePendingAcksUpToPositionAndDecrementUnacked(1L, 1L);

        assertEquals(sharedConsumer.getUnackedMessages(), 0);
        assertFalse(sharedConsumer.getPendingAcks().contains(1L, 1L));
    }

    private Consumer newSharedPersistentConsumer(PersistentSubscription subscription, boolean batchIndexAckEnabled) {
        ServerCnx cnx = mock(ServerCnx.class);
        SocketAddress address = mock(SocketAddress.class);
        PersistentTopic topic = mock(PersistentTopic.class);
        BrokerService brokerService = mock(BrokerService.class);
        PulsarService pulsarService = mock(PulsarService.class);
        ServiceConfiguration serviceConfiguration = mock(ServiceConfiguration.class);
        HierarchyTopicPolicies topicPolicies = new HierarchyTopicPolicies();
        topicPolicies.getMaxUnackedMessagesOnConsumer().updateBrokerValue(0);

        when(cnx.clientAddress()).thenReturn(address);
        when(subscription.getTopic()).thenReturn(topic);
        when(topic.getBrokerService()).thenReturn(brokerService);
        when(topic.getHierarchyTopicPolicies()).thenReturn(topicPolicies);
        when(brokerService.getPulsar()).thenReturn(pulsarService);
        when(pulsarService.getConfiguration()).thenReturn(serviceConfiguration);
        when(serviceConfiguration.isAcknowledgmentAtBatchIndexLevelEnabled()).thenReturn(batchIndexAckEnabled);

        return new Consumer(subscription, Shared, "topic", 1, 0, "Cons1", true, cnx, "myrole-1",
                emptyMap(), false, new KeySharedMeta().setKeySharedMode(AUTO_SPLIT), latest, DEFAULT_CONSUMER_EPOCH);
    }
}
