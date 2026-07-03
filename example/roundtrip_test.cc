// Smoke + round-trip test for the generated DTO and conversion code.
// Builds a proto, converts proto -> DTO -> proto, and asserts equality.
#include <cassert>
#include <iostream>

#include "bank.conv.h"
#include "bank.fmt.h"

int main() {
  namespace pb = example::bank;
  namespace dto = example::bank::dto;

  // Build a fully populated proto message.
  pb::Account proto;
  proto.set_id("acc-1");
  proto.set_type(pb::SAVINGS);
  proto.set_balance_cents(123456);
  proto.mutable_address()->set_street("1 Main St");
  proto.mutable_address()->set_city("Springfield");
  proto.mutable_address()->set_zip("12345");
  proto.add_tags("a");
  proto.add_tags("b");
  (*proto.mutable_counters())["logins"] = 7;
  pb::Address related;
  related.set_street("2 Side St");
  related.set_city("Shelbyville");
  (*proto.mutable_related())["billing"] = related;
  proto.set_phone("+1-555-0100");  // oneof
  proto.set_interest_rate(1.5);
  proto.add_allowed_types(pb::CHECKING);
  proto.add_allowed_types(pb::SAVINGS);

  // proto -> DTO
  dto::Account d;
  from_proto(proto, d);

  assert(d.id == "acc-1");
  assert(d.type == dto::AccountType::SAVINGS);
  assert(d.balance_cents == 123456);
  assert(d.address.has_value());
  assert(d.address->zip.has_value() && *d.address->zip == "12345");
  assert(d.tags.size() == 2 && d.tags[1] == "b");
  assert(d.counters.at("logins") == 7);
  assert(d.related.at("billing").city == "Shelbyville");
  assert(d.contact.index() == 2);  // monostate=0, email=1, phone=2
  assert(std::get<2>(d.contact) == "+1-555-0100");
  assert(d.interest_rate.has_value() && *d.interest_rate == 1.5);
  assert(d.allowed_types.size() == 2);

  // DTO -> proto, then compare with the original.
  pb::Account back;
  to_proto(d, &back);

  std::string a, b;
  proto.SerializeToString(&a);
  back.SerializeToString(&b);
  assert(a == b && "round-trip serialization mismatch");

  // Default-constructed DTO must be zero-initialized (no indeterminate values).
  dto::Account empty;
  assert(empty.balance_cents == 0);
  assert(empty.type == dto::AccountType::ACCOUNT_TYPE_UNSPECIFIED);
  assert(empty.contact.index() == 0);

  // Formatter: enum names, optional, oneof-by-index, and nested types render.
  std::string s = logfmt::to_string_one_line(d);
  assert(s.find("type: SAVINGS") != std::string::npos);          // enum name
  assert(s.find("contact.phone: +1-555-0100") != std::string::npos);  // oneof label
  assert(s.find("zip: 12345") != std::string::npos);             // nested optional
  std::string es = logfmt::to_string_one_line(empty);
  assert(es.find("contact: (unset)") != std::string::npos);      // monostate
  assert(es.find("address: null") != std::string::npos);         // empty optional
  std::cout << logfmt::to_string(d) << "\n";

  std::cout << "round-trip test passed\n";
  return 0;
}
