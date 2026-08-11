#
# Licensed to the Apache Software Foundation (ASF) under one or more
# contributor license agreements.  See the NOTICE file distributed with
# this work for additional information regarding copyright ownership.
# The ASF licenses this file to You under the Apache License, Version 2.0
# (the "License"); you may not use this file except in compliance with
# the License.  You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

=encoding utf-8

ai-proxy / ai-proxy-multi must record $apisix_upstream_response_time and
$llm_time_to_first_token in integer milliseconds on both success and upstream
error (429/5xx) paths, using the per-attempt request-start clock. Transport
failures still stamp integer-ms upstream time with TTFT left at 0.

=cut

use t::APISIX 'no_plan';

log_level("info");
repeat_each(1);
no_long_string();
no_root_location();


add_block_preprocessor(sub {
    my ($block) = @_;

    if (!defined $block->request) {
        $block->set_value("request", "GET /t");
    }
});

run_tests();

__DATA__

=== TEST 1: set route with log-phase latency checker (served responses)
--- config
    location /t {
        content_by_lua_block {
            local t = require("lib.test_admin").test
            local code, body = t('/apisix/admin/routes/1',
                ngx.HTTP_PUT,
                [[{
                    "uri": "/anything",
                    "plugins": {
                        "ai-proxy": {
                            "provider": "openai",
                            "auth": {
                                "header": {
                                    "Authorization": "Bearer test-key"
                                }
                            },
                            "options": {
                                "model": "gpt-4"
                            },
                            "override": {
                                "endpoint": "http://127.0.0.1:1980"
                            },
                            "ssl_verify": false
                        },
                        "serverless-post-function": {
                            "phase": "log",
                            "functions": ["return function(_, ctx) local urt_raw = ctx.var.apisix_upstream_response_time local ttft_raw = ctx.var.llm_time_to_first_token local urt = tonumber(urt_raw) local ttft = tonumber(ttft_raw) local urt_str = tostring(urt_raw or '') if urt and ttft and urt == math.floor(urt) and ttft == math.floor(ttft) and not urt_str:find('.', 1, true) and urt >= 100 and ttft >= 100 and urt == ttft then ngx.log(ngx.WARN, 'LAT_OK served urt=', urt, ' ttft=', ttft) else ngx.log(ngx.ERR, 'LAT_FAIL served urt=', tostring(urt_raw), ' ttft=', tostring(ttft_raw)) end end"]
                        }
                    }
                }]]
            )

            if code >= 300 then
                ngx.status = code
            end
            ngx.say(body)
        }
    }
--- response_body
passed



=== TEST 2: 200 after ~200ms uses integer millisecond clock for both vars
--- request
POST /anything
{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}
--- more_headers
X-AI-Fixture: openai/chat-basic.json
X-AI-Fixture-Delay: 200
--- error_code: 200
--- error_log
LAT_OK served
--- no_error_log
LAT_FAIL



=== TEST 3: upstream 500 after ~200ms stays on the same millisecond scale
--- request
POST /anything
{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}
--- more_headers
X-AI-Fixture: openai/chat-basic.json
X-AI-Fixture-Delay: 200
X-AI-Fixture-Status: 500
--- error_code: 500
--- error_log
LAT_OK served
--- no_error_log
LAT_FAIL



=== TEST 4: upstream 429 after ~200ms stays on the same millisecond scale
--- request
POST /anything
{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}
--- more_headers
X-AI-Fixture: openai/chat-basic.json
X-AI-Fixture-Delay: 200
X-AI-Fixture-Status: 429
--- error_code: 429
--- error_log
LAT_OK served
--- no_error_log
LAT_FAIL



=== TEST 5: set route with log-phase checker (transport failure)
--- config
    location /t {
        content_by_lua_block {
            local t = require("lib.test_admin").test
            local code, body = t('/apisix/admin/routes/1',
                ngx.HTTP_PUT,
                [[{
                    "uri": "/anything",
                    "plugins": {
                        "ai-proxy": {
                            "provider": "openai",
                            "auth": {
                                "header": {
                                    "Authorization": "Bearer test-key"
                                }
                            },
                            "options": {
                                "model": "gpt-4"
                            },
                            "override": {
                                "endpoint": "http://127.0.0.1:1979"
                            },
                            "timeout": 1000,
                            "ssl_verify": false
                        },
                        "serverless-post-function": {
                            "phase": "log",
                            "functions": ["return function(_, ctx) local urt_raw = ctx.var.apisix_upstream_response_time local ttft_raw = ctx.var.llm_time_to_first_token local urt = tonumber(urt_raw) local ttft = tonumber(ttft_raw) or 0 local urt_str = tostring(urt_raw or '') if urt and urt == math.floor(urt) and urt >= 0 and not urt_str:find('.', 1, true) and ttft == 0 then ngx.log(ngx.WARN, 'LAT_OK transport urt=', urt, ' ttft=', ttft) else ngx.log(ngx.ERR, 'LAT_FAIL transport urt=', tostring(urt_raw), ' ttft=', tostring(ttft_raw)) end end"]
                        }
                    }
                }]]
            )

            if code >= 300 then
                ngx.status = code
            end
            ngx.say(body)
        }
    }
--- response_body
passed



=== TEST 6: unreachable upstream keeps integer-ms urt and TTFT at 0
--- request
POST /anything
{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}
--- error_code: 500
--- error_log
LAT_OK transport
--- no_error_log
LAT_FAIL
