/*
 * Copyright (c) 2018, Daniel Gultsch All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without modification,
 * are permitted provided that the following conditions are met:
 *
 * 1. Redistributions of source code must retain the above copyright notice, this
 * list of conditions and the following disclaimer.
 *
 * 2. Redistributions in binary form must reproduce the above copyright notice,
 * this list of conditions and the following disclaimer in the documentation and/or
 * other materials provided with the distribution.
 *
 * 3. Neither the name of the copyright holder nor the names of its contributors
 * may be used to endorse or promote products derived from this software without
 * specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
 * ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
 * WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
 * DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE FOR
 * ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
 * SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package eu.siacs.conversations.ui.util;

import eu.siacs.conversations.entities.IndividualMessage;
import eu.siacs.conversations.entities.Message;
import java.util.ArrayList;
import java.util.Calendar;
import java.util.List;

public class DateSeparator {

    public static void addAll(List<Message> messages) {
        if (messages.isEmpty()) {
            return;
        }
        // build into a scratch list. inserting separators mid list is quadratic on
        // ArrayList because every insert shifts the tail
        final var separated = new ArrayList<Message>(messages.size() + 8);
        final var calendar = Calendar.getInstance();
        int lastDayKey = -1;
        for (final Message message : messages) {
            calendar.setTimeInMillis(message.getTimeSent());
            final int dayKey =
                    calendar.get(Calendar.YEAR) * 1000 + calendar.get(Calendar.DAY_OF_YEAR);
            if (dayKey != lastDayKey) {
                separated.add(IndividualMessage.createDateSeparator(message));
                lastDayKey = dayKey;
            }
            separated.add(message);
        }
        messages.clear();
        messages.addAll(separated);
    }
}
